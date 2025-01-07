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

func main() {
	builder := dashboard.NewDashboardBuilder("CloudCost Exporter").
		Id(10297).
		Uid("1a9c0de366458599246184cf0ae8b468").
		Title("CloudCost Exporter").
		Editable().
		Tooltip(1).
		Timepicker(dashboard.NewTimePickerBuilder()).
		Refresh("30s").
		Version(0x2).
		Variables([]cog.Builder[dashboard.VariableModel]{dashboard.NewDatasourceVariableBuilder("datasource").
			Name("datasource").
			Label("Data Source").
			Type("prometheus"),
			dashboard.NewQueryVariableBuilder("cluster").
				Name("cluster").
				Query(dashboard.StringOrMap{Map: map[string]interface{}{"query": "label_values(tanka_environment_info{app=\"cloudcost-exporter\"},exported_cluster)", "refId": "PrometheusVariableQueryEditor-VariableQuery"}}).
				Datasource(dashboard.DataSourceRef{Type: cog.ToPtr[string]("prometheus"), Uid: cog.ToPtr[string]("${datasource}")}).
				Current(dashboard.VariableOption{Text: dashboard.StringOrArrayOfString{ArrayOfString: []string{"dev-us-central-0", "dev-us-east-0", "prod-us-east-0"}}, Value: dashboard.StringOrArrayOfString{ArrayOfString: []string{"dev-us-central-0", "dev-us-east-0", "prod-us-east-0"}}}).
				Multi(true).
				Refresh(1).
				IncludeAll(true).
				AllValue(".*")}).
		Annotations([]cog.Builder[dashboard.AnnotationQuery]{dashboard.NewAnnotationQueryBuilder().
			Name("Annotations & Alerts").
			Datasource(dashboard.DataSourceRef{Type: cog.ToPtr[string]("grafana"), Uid: cog.ToPtr[string]("-- Grafana --")}).
			Hide(true).
			IconColor("rgba(0, 211, 255, 1)").
			Type("dashboard").
			BuiltIn(1)}).
		Preload(false).
		WithRow(dashboard.NewRowBuilder("Overview").
			Title("Overview").
			GridPos(dashboard.GridPos{H: 1, W: 24, X: 0, Y: 0}).
			Id(0x8)).
		WithPanel(buildUpPanel()).
		WithPanel(buildCollectorScrapeDurationPanel()).
		WithRow(dashboard.NewRowBuilder("AWS").
			Title("AWS").
			GridPos(dashboard.GridPos{H: 1, W: 24, X: 0, Y: 15}).
			Id(0x7)).
		WithPanel(buildCostExplorerAPIRequestsPanel()).
		WithPanel(buildAWSS3NextScrapePanel()).
		WithRow(dashboard.NewRowBuilder("GCP").
			Title("GCP").
			GridPos(dashboard.GridPos{H: 1, W: 24, X: 0, Y: 24}).
			Id(0x2)).
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

func buildCollectorScrapeDurationPanel() *timeseries.PanelBuilder {
	return timeseries.NewPanelBuilder().
		Id(0xd).
		Targets([]cog.Builder[variants.Dataquery]{prometheus.NewDataqueryBuilder().
			Expr("cloudcost_exporter_collector_last_scrape_duration_seconds").
			Range().
			EditorMode("code").
			LegendFormat("{{provider}}:{{collector}}").
			RefId("A").
			Datasource(dashboard.DataSourceRef{Type: cog.ToPtr[string]("prometheus"), Uid: cog.ToPtr[string]("${datasource}")}),
			prometheus.NewDataqueryBuilder().
				Expr("vector(60)").
				Range().
				EditorMode("code").
				LegendFormat("scrape_interval").
				RefId("B").
				Hide(false).
				Datasource(dashboard.DataSourceRef{Type: cog.ToPtr[string]("prometheus"), Uid: cog.ToPtr[string]("${datasource}")})}).
		Title("Collector Scrape Duration").
		Description("Duration of scrapes by provider and collector").
		Datasource(dashboard.DataSourceRef{Type: cog.ToPtr[string]("prometheus"), Uid: cog.ToPtr[string]("${datasource}")}).
		GridPos(dashboard.GridPos{H: 8, W: 24, X: 0, Y: 7}).
		Height(0x8).
		Span(0x18).
		Unit("s").
		Thresholds(dashboard.NewThresholdsConfigBuilder().
			Mode("absolute").
			Steps([]dashboard.Threshold{dashboard.Threshold{Color: "green"},
				dashboard.Threshold{Value: cog.ToPtr[float64](80), Color: "red"}})).
		ColorScheme(dashboard.NewFieldColorBuilder().
			Mode("palette-classic")).
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
		ThresholdsStyle(common.NewGraphThresholdsStyleConfigBuilder().
			Mode("off")).
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
		Id(0x6).
		Targets([]cog.Builder[variants.Dataquery]{prometheus.NewDataqueryBuilder().
			Expr("sum by (cluster) (increase(cloudcost_exporter_aws_s3_cost_api_requests_total{cluster=~\"$cluster\"}[5m]))").
			Range().
			EditorMode("code").
			LegendFormat("__auto").
			RefId("A").
			Datasource(dashboard.DataSourceRef{Type: cog.ToPtr[string]("prometheus"), Uid: cog.ToPtr[string]("000000134")})}).
		Title("CostExplorer API Requests").
		Datasource(dashboard.DataSourceRef{Type: cog.ToPtr[string]("prometheus"), Uid: cog.ToPtr[string]("${datasource}")}).
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
		Id(0x9).
		Targets([]cog.Builder[variants.Dataquery]{prometheus.NewDataqueryBuilder().
			Expr("max by (cluster) (cloudcost_exporter_aws_s3_next_scrape{cluster=~\"$cluster\"}) - time() ").
			Range().
			EditorMode("code").
			LegendFormat("__auto").
			RefId("A").
			Datasource(dashboard.DataSourceRef{Type: cog.ToPtr[string]("prometheus"), Uid: cog.ToPtr[string]("000000134")})}).
		Title("Next pricing map refresh").
		Description("The AWS s3 module uses cost data pulled from Cost Explorer, which costs $0.01 per API call. The cost metrics are refreshed every hour, so if this value goes below 0, it indicates a problem with refreshing the pricing map and thus needs investigation.").
		Datasource(dashboard.DataSourceRef{Type: cog.ToPtr[string]("prometheus"), Uid: cog.ToPtr[string]("${datasource}")}).
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
		Id(0x3).
		Targets([]cog.Builder[variants.Dataquery]{prometheus.NewDataqueryBuilder().
			Expr("sum by (cluster, status) (increase(cloudcost_exporter_gcp_gcs_bucket_list_status_total[5m]))").
			Range().
			EditorMode("code").
			LegendFormat("{{cluster}}:{{status}}").
			RefId("A").
			Datasource(dashboard.DataSourceRef{Type: cog.ToPtr[string]("prometheus"), Uid: cog.ToPtr[string]("000000134")})}).
		Title("GCS List Buckets Requests Per Second").
		Datasource(dashboard.DataSourceRef{Type: cog.ToPtr[string]("prometheus"), Uid: cog.ToPtr[string]("${datasource}")}).
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
		Id(0xa).
		Targets([]cog.Builder[variants.Dataquery]{prometheus.NewDataqueryBuilder().
			Expr("max by (cluster) (cloudcost_exporter_gcp_gcs_next_scrape{cluster=~\"$cluster\"}) - time() ").
			Range().
			EditorMode("code").
			LegendFormat("__auto").
			RefId("A").
			Datasource(dashboard.DataSourceRef{Type: cog.ToPtr[string]("prometheus"), Uid: cog.ToPtr[string]("000000134")})}).
		Title("GCS Pricing Map Refresh Time").
		Description("The amount of time before the next refresh of the GCS pricing map. ").
		Datasource(dashboard.DataSourceRef{Type: cog.ToPtr[string]("prometheus"), Uid: cog.ToPtr[string]("${datasource}")}).
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

func buildUpPanel() *stat.PanelBuilder {
	return stat.NewPanelBuilder().
		Id(0xc).
		Targets([]cog.Builder[variants.Dataquery]{prometheus.NewDataqueryBuilder().
			Expr("max by (provider, collector) (cloudcost_exporter_collector_last_scrape_error == 0)").
			Range().
			EditorMode("code").
			LegendFormat("{{provider}}:{{collector}}").
			RefId("A").
			Datasource(dashboard.DataSourceRef{Type: cog.ToPtr[string]("prometheus"), Uid: cog.ToPtr[string]("${datasource}")})}).
		Title("Collector Status").
		Description("Display the status of all the collectors running.").
		Datasource(dashboard.DataSourceRef{Type: cog.ToPtr[string]("prometheus"), Uid: cog.ToPtr[string]("${datasource}")}).
		GridPos(dashboard.GridPos{H: 6, W: 11, X: 0, Y: 1}).
		Height(0x6).
		Span(0xb).
		Unit("short").
		Mappings([]dashboard.ValueMapping{dashboard.ValueMapOrRangeMapOrRegexMapOrSpecialValueMap{ValueMap: cog.ToPtr[dashboard.ValueMap](dashboard.ValueMap{Type: "value", Options: map[string]dashboard.ValueMappingResult{"0": dashboard.ValueMappingResult{Text: cog.ToPtr[string]("Up"), Index: cog.ToPtr[int32](0)}}})},
			dashboard.ValueMapOrRangeMapOrRegexMapOrSpecialValueMap{SpecialValueMap: cog.ToPtr[dashboard.SpecialValueMap](dashboard.SpecialValueMap{Type: "special", Options: dashboard.DashboardSpecialValueMapOptions{Match: "null+nan", Result: dashboard.ValueMappingResult{Text: cog.ToPtr[string]("Down"), Color: cog.ToPtr[string]("red"), Index: cog.ToPtr[int32](1)}}})}}).
		Thresholds(dashboard.NewThresholdsConfigBuilder().
			Mode("absolute").
			Steps([]dashboard.Threshold{dashboard.Threshold{Color: "green"},
				dashboard.Threshold{Value: cog.ToPtr[float64](80), Color: "red"}})).
		ColorScheme(dashboard.NewFieldColorBuilder().
			Mode("thresholds")).
		GraphMode("area").
		ColorMode("value").
		JustifyMode("auto").
		TextMode("auto").
		ReduceOptions(common.NewReduceDataOptionsBuilder().
			Values(false).
			Calcs([]string{"lastNotNull"})).
		PercentChangeColorMode("standard").
		Orientation("auto")
}
