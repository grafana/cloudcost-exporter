package main

import (
	"encoding/json"
	"fmt"

	"github.com/grafana/grafana-foundation-sdk/go/cog"
	"github.com/grafana/grafana-foundation-sdk/go/cog/variants"
	"github.com/grafana/grafana-foundation-sdk/go/common"
	"github.com/grafana/grafana-foundation-sdk/go/dashboard"
	"github.com/grafana/grafana-foundation-sdk/go/prometheus"
	"github.com/grafana/grafana-foundation-sdk/go/stat"
	"github.com/grafana/grafana-foundation-sdk/go/timeseries"
)

func prometheusDatasourceRef() dashboard.DataSourceRef {
	return dashboard.DataSourceRef{
		Type: cog.ToPtr[string]("prometheus"),
		Uid:  cog.ToPtr[string]("${datasource}"),
	}
}

func prometheusQuery(expression string, legendFormat string) *prometheus.DataqueryBuilder {
	return prometheus.NewDataqueryBuilder().
		Expr(expression).
		Range().
		LegendFormat(legendFormat)
}

func main() {
	builder := dashboard.NewDashboardBuilder("CloudCost Exporter").
		// leaving this for BC reasons, but a proper human-readable UID would be better.
		Uid("1a9c0de366458599246184cf0ae8b468").
		Editable().
		Tooltip(dashboard.DashboardCursorSyncCrosshair).
		Refresh("30s").
		WithVariable(dashboard.NewDatasourceVariableBuilder("datasource").
			Label("Data Source").
			Type("prometheus"),
		).
		WithVariable(dashboard.NewQueryVariableBuilder("cluster").
			// TODO: this looks grafana-specific
			Query(dashboard.StringOrMap{
				String: cog.ToPtr("label_values(tanka_environment_info{app=\"cloudcost-exporter\"},exported_cluster)"),
			}).
			Datasource(prometheusDatasourceRef()).
			Multi(true).
			Refresh(dashboard.VariableRefreshOnDashboardLoad).
			IncludeAll(true).
			AllValue(".*"),
		).
		Annotation(dashboard.NewAnnotationQueryBuilder().
			Name("Annotations & Alerts").
			Datasource(dashboard.DataSourceRef{
				Type: cog.ToPtr[string]("grafana"),
				Uid:  cog.ToPtr[string]("-- Grafana --"),
			}).
			Hide(true).
			IconColor("rgba(0, 211, 255, 1)").
			Type("dashboard").
			BuiltIn(1),
		).
		WithRow(dashboard.NewRowBuilder("Overview")).
		WithPanel(collectorStatusCurrent().Height(6).Span(11)).
		WithPanel(collectorScrapeDurationOverTime()).
		WithRow(dashboard.NewRowBuilder("AWS").
			Title("AWS").
			GridPos(dashboard.GridPos{H: 1, W: 24, X: 0, Y: 15})).
		WithPanel(buildCostExplorerAPIRequestsPanel()).
		WithPanel(buildAWSS3NextScrapePanel()).
		WithRow(dashboard.NewRowBuilder("GCP").
			Title("GCP").
			GridPos(dashboard.GridPos{H: 1, W: 24, X: 0, Y: 24})).
		WithPanel(buildGCPlistBucketsRPSPanel()).
		WithPanel(buildNextScrapePanel())

	sampleDashboard, err := builder.Build()
	if err != nil {
		panic(err)
	}
	dashboardJson, err := json.MarshalIndent(sampleDashboard, "", "  ")
	if err != nil {
		panic(err)
	}

	fmt.Println(string(dashboardJson))
}

func collectorScrapeDurationOverTime() *timeseries.PanelBuilder {
	return timeseries.NewPanelBuilder().
		Title("Collector Scrape Duration").
		Description("Duration of scrapes by provider and collector").
		Datasource(prometheusDatasourceRef()).
		WithTarget(
			prometheusQuery("cloudcost_exporter_collector_last_scrape_duration_seconds", "{{provider}}:{{collector}}"),
		).
		GridPos(dashboard.GridPos{H: 8, W: 24, X: 0, Y: 7}).
		Height(0x8).
		Span(0x18).
		Unit("s").
		ThresholdsStyle(common.NewGraphThresholdsStyleConfigBuilder().Mode(common.GraphThresholdsStyleModeLine)).
		Thresholds(dashboard.NewThresholdsConfigBuilder().
			Mode(dashboard.ThresholdsModeAbsolute).
			Steps([]dashboard.Threshold{
				{Color: "green"},
				{Value: cog.ToPtr[float64](60), Color: "red"},
			}),
		).
		ColorScheme(dashboard.NewFieldColorBuilder().Mode("palette-classic")).
		Legend(common.NewVizLegendOptionsBuilder().
			DisplayMode("table").
			Placement("bottom").
			ShowLegend(true).
			SortBy("Last *").
			SortDesc(true).
			Calcs([]string{"lastNotNull",
				"min",
				"max"})).
		Tooltip(common.NewVizTooltipOptionsBuilder().
			Mode("single").
			Sort("none")).
		DrawStyle("line").
		GradientMode("none").
		LineWidth(1).
		LineInterpolation("linear").
		FillOpacity(0).
		ShowPoints("auto").
		PointSize(4).
		AxisPlacement("auto").
		AxisColorMode("text").
		ScaleDistribution(common.NewScaleDistributionConfigBuilder().
			Type("linear")).
		AxisCenteredZero(false).
		BarAlignment(0).
		BarWidthFactor(0.6).
		Stacking(common.NewStackingConfigBuilder().
			Mode("none").
			Group("A")).
		HideFrom(common.NewHideSeriesConfigBuilder().
			Tooltip(false).
			Legend(false).
			Viz(false)).
		InsertNulls(common.BoolOrFloat64{Bool: cog.ToPtr[bool](false)}).
		SpanNulls(common.BoolOrFloat64{Bool: cog.ToPtr[bool](false)}).
		AxisBorderShow(false)
}

func buildCostExplorerAPIRequestsPanel() *timeseries.PanelBuilder {
	return timeseries.NewPanelBuilder().
		Targets([]cog.Builder[variants.Dataquery]{prometheus.NewDataqueryBuilder().
			Expr("sum by (cluster) (increase(cloudcost_exporter_aws_s3_cost_api_requests_total{cluster=~\"$cluster\"}[5m]))").
			Range().
			EditorMode("code").
			LegendFormat("__auto").
			RefId("A").
			Datasource(dashboard.DataSourceRef{Type: cog.ToPtr[string]("prometheus"), Uid: cog.ToPtr[string]("000000134")})}).
		Title("CostExplorer API Requests").
		Datasource(prometheusDatasourceRef()).
		GridPos(dashboard.GridPos{H: 8, W: 11, X: 0, Y: 16}).
		Height(0x8).
		Span(0xb).
		Unit("reqps").
		Thresholds(dashboard.NewThresholdsConfigBuilder().
			Mode("absolute").
			Steps([]dashboard.Threshold{dashboard.Threshold{Color: "green"},
				dashboard.Threshold{Value: cog.ToPtr[float64](80), Color: "red"}})).
		ColorScheme(dashboard.NewFieldColorBuilder().
			Mode("palette-classic")).
		Legend(common.NewVizLegendOptionsBuilder().
			DisplayMode("list").
			Placement("bottom").
			ShowLegend(true)).
		Tooltip(common.NewVizTooltipOptionsBuilder().
			Mode("single").
			Sort("none")).
		DrawStyle("line").
		GradientMode("none").
		ThresholdsStyle(common.NewGraphThresholdsStyleConfigBuilder().
			Mode("off")).
		LineWidth(1).
		LineInterpolation("linear").
		FillOpacity(0).
		ShowPoints("auto").
		PointSize(5).
		AxisPlacement("auto").
		AxisColorMode("text").
		ScaleDistribution(common.NewScaleDistributionConfigBuilder().
			Type("linear")).
		AxisCenteredZero(false).
		BarAlignment(0).
		BarWidthFactor(0.6).
		Stacking(common.NewStackingConfigBuilder().
			Mode("none").
			Group("A")).
		HideFrom(common.NewHideSeriesConfigBuilder().
			Tooltip(false).
			Legend(false).
			Viz(false)).
		InsertNulls(common.BoolOrFloat64{Bool: cog.ToPtr[bool](false)}).
		SpanNulls(common.BoolOrFloat64{Bool: cog.ToPtr[bool](false)}).
		AxisBorderShow(false)
}

func buildAWSS3NextScrapePanel() *timeseries.PanelBuilder {
	return timeseries.NewPanelBuilder().
		Targets([]cog.Builder[variants.Dataquery]{prometheus.NewDataqueryBuilder().
			Expr("max by (cluster) (cloudcost_exporter_aws_s3_next_scrape{cluster=~\"$cluster\"}) - time() ").
			Range().
			EditorMode("code").
			LegendFormat("__auto").
			RefId("A").
			Datasource(dashboard.DataSourceRef{Type: cog.ToPtr[string]("prometheus"), Uid: cog.ToPtr[string]("000000134")})}).
		Title("Next pricing map refresh").
		Description("The AWS s3 module uses cost data pulled from Cost Explorer, which costs $0.01 per API call. The cost metrics are refreshed every hour, so if this value goes below 0, it indicates a problem with refreshing the pricing map and thus needs investigation.").
		Datasource(prometheusDatasourceRef()).
		GridPos(dashboard.GridPos{H: 8, W: 13, X: 11, Y: 16}).
		Height(0x8).
		Span(0xd).
		Unit("s").
		Thresholds(dashboard.NewThresholdsConfigBuilder().
			Mode("absolute").
			Steps([]dashboard.Threshold{dashboard.Threshold{Color: "green"},
				dashboard.Threshold{Value: cog.ToPtr[float64](80), Color: "red"}})).
		ColorScheme(dashboard.NewFieldColorBuilder().
			Mode("palette-classic")).
		Legend(common.NewVizLegendOptionsBuilder().
			DisplayMode("list").
			Placement("bottom").
			ShowLegend(true)).
		Tooltip(common.NewVizTooltipOptionsBuilder().
			Mode("single").
			Sort("none")).
		DrawStyle("line").
		GradientMode("none").
		ThresholdsStyle(common.NewGraphThresholdsStyleConfigBuilder().
			Mode("off")).
		LineWidth(1).
		LineInterpolation("linear").
		FillOpacity(0).
		ShowPoints("auto").
		PointSize(5).
		AxisPlacement("auto").
		AxisColorMode("text").
		ScaleDistribution(common.NewScaleDistributionConfigBuilder().
			Type("linear")).
		AxisCenteredZero(false).
		BarAlignment(0).
		BarWidthFactor(0.6).
		Stacking(common.NewStackingConfigBuilder().
			Mode("none").
			Group("A")).
		HideFrom(common.NewHideSeriesConfigBuilder().
			Tooltip(false).
			Legend(false).
			Viz(false)).
		InsertNulls(common.BoolOrFloat64{Bool: cog.ToPtr[bool](false)}).
		SpanNulls(common.BoolOrFloat64{Bool: cog.ToPtr[bool](false)}).
		AxisBorderShow(false)
}

func buildGCPlistBucketsRPSPanel() *timeseries.PanelBuilder {
	return timeseries.NewPanelBuilder().
		Targets([]cog.Builder[variants.Dataquery]{prometheus.NewDataqueryBuilder().
			Expr("sum by (cluster, status) (increase(cloudcost_exporter_gcp_gcs_bucket_list_status_total[5m]))").
			Range().
			EditorMode("code").
			LegendFormat("{{cluster}}:{{status}}").
			RefId("A").
			Datasource(dashboard.DataSourceRef{Type: cog.ToPtr[string]("prometheus"), Uid: cog.ToPtr[string]("000000134")})}).
		Title("GCS List Buckets Requests Per Second").
		Datasource(prometheusDatasourceRef()).
		GridPos(dashboard.GridPos{H: 7, W: 11, X: 0, Y: 25}).
		Height(0x7).
		Span(0xb).
		Unit("reqps").
		Thresholds(dashboard.NewThresholdsConfigBuilder().
			Mode("absolute").
			Steps([]dashboard.Threshold{dashboard.Threshold{Color: "green"},
				dashboard.Threshold{Value: cog.ToPtr[float64](80), Color: "red"}})).
		ColorScheme(dashboard.NewFieldColorBuilder().
			Mode("palette-classic")).
		Legend(common.NewVizLegendOptionsBuilder().
			DisplayMode("list").
			Placement("bottom").
			ShowLegend(true)).
		Tooltip(common.NewVizTooltipOptionsBuilder().
			Mode("single").
			Sort("none")).
		DrawStyle("line").
		GradientMode("none").
		ThresholdsStyle(common.NewGraphThresholdsStyleConfigBuilder().
			Mode("off")).
		LineWidth(1).
		LineInterpolation("linear").
		FillOpacity(0).
		ShowPoints("auto").
		PointSize(5).
		AxisPlacement("auto").
		AxisColorMode("text").
		ScaleDistribution(common.NewScaleDistributionConfigBuilder().
			Type("linear")).
		AxisCenteredZero(false).
		BarAlignment(0).
		BarWidthFactor(0.6).
		Stacking(common.NewStackingConfigBuilder().
			Mode("none").
			Group("A")).
		HideFrom(common.NewHideSeriesConfigBuilder().
			Tooltip(false).
			Legend(false).
			Viz(false)).
		InsertNulls(common.BoolOrFloat64{Bool: cog.ToPtr[bool](false)}).
		SpanNulls(common.BoolOrFloat64{Bool: cog.ToPtr[bool](false)}).
		AxisBorderShow(false)
}

func buildNextScrapePanel() *timeseries.PanelBuilder {
	return timeseries.NewPanelBuilder().
		Targets([]cog.Builder[variants.Dataquery]{prometheus.NewDataqueryBuilder().
			Expr("max by (cluster) (cloudcost_exporter_gcp_gcs_next_scrape{cluster=~\"$cluster\"}) - time() ").
			Range().
			EditorMode("code").
			LegendFormat("__auto").
			RefId("A").
			Datasource(dashboard.DataSourceRef{Type: cog.ToPtr[string]("prometheus"), Uid: cog.ToPtr[string]("000000134")})}).
		Title("GCS Pricing Map Refresh Time").
		Description("The amount of time before the next refresh of the GCS pricing map. ").
		Datasource(prometheusDatasourceRef()).
		GridPos(dashboard.GridPos{H: 7, W: 13, X: 11, Y: 25}).
		Height(0x7).
		Span(0xd).
		Unit("s").
		Thresholds(dashboard.NewThresholdsConfigBuilder().
			Mode("absolute").
			Steps([]dashboard.Threshold{dashboard.Threshold{Color: "green"},
				dashboard.Threshold{Value: cog.ToPtr[float64](80), Color: "red"}})).
		ColorScheme(dashboard.NewFieldColorBuilder().
			Mode("palette-classic")).
		Legend(common.NewVizLegendOptionsBuilder().
			DisplayMode("list").
			Placement("bottom").
			ShowLegend(true)).
		Tooltip(common.NewVizTooltipOptionsBuilder().
			Mode("single").
			Sort("none")).
		DrawStyle("line").
		GradientMode("none").
		ThresholdsStyle(common.NewGraphThresholdsStyleConfigBuilder().
			Mode("off")).
		LineWidth(1).
		LineInterpolation("linear").
		FillOpacity(0).
		ShowPoints("auto").
		PointSize(5).
		AxisPlacement("auto").
		AxisColorMode("text").
		ScaleDistribution(common.NewScaleDistributionConfigBuilder().
			Type("linear")).
		AxisCenteredZero(false).
		BarAlignment(0).
		BarWidthFactor(0.6).
		Stacking(common.NewStackingConfigBuilder().
			Mode("none").
			Group("A")).
		HideFrom(common.NewHideSeriesConfigBuilder().
			Tooltip(false).
			Legend(false).
			Viz(false)).
		InsertNulls(common.BoolOrFloat64{Bool: cog.ToPtr[bool](false)}).
		SpanNulls(common.BoolOrFloat64{Bool: cog.ToPtr[bool](false)}).
		AxisBorderShow(false)
}

func collectorStatusCurrent() *stat.PanelBuilder {
	return stat.NewPanelBuilder().
		Title("Collector Status").
		Description("Display the status of all the collectors running.").
		Datasource(prometheusDatasourceRef()).
		Unit("short").
		WithTarget(
			prometheusQuery("max by (provider, collector) (cloudcost_exporter_collector_last_scrape_error == 0)", "{{provider}}:{{collector}}").
				Instant(),
		).
		JustifyMode(common.BigValueJustifyModeAuto).
		TextMode(common.BigValueTextModeAuto).
		Orientation(common.VizOrientationAuto).
		ReduceOptions(common.NewReduceDataOptionsBuilder().
			Values(false).
			Calcs([]string{"lastNotNull"}),
		).
		Mappings([]dashboard.ValueMapping{
			{
				ValueMap: cog.ToPtr[dashboard.ValueMap](dashboard.ValueMap{
					Type: "value",
					Options: map[string]dashboard.ValueMappingResult{
						"0": {Text: cog.ToPtr[string]("Up"), Index: cog.ToPtr[int32](0)},
					},
				}),
			},
			{
				SpecialValueMap: cog.ToPtr[dashboard.SpecialValueMap](dashboard.SpecialValueMap{
					Type: "special",
					Options: dashboard.DashboardSpecialValueMapOptions{
						Match: "null+nan",
						Result: dashboard.ValueMappingResult{
							Text:  cog.ToPtr[string]("Down"),
							Color: cog.ToPtr[string]("red"),
							Index: cog.ToPtr[int32](1),
						},
					},
				}),
			},
		})
}
