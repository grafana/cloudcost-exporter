package dashboards

import (
	"github.com/grafana/grafana-foundation-sdk/go/cog"
	"github.com/grafana/grafana-foundation-sdk/go/common"
	"github.com/grafana/grafana-foundation-sdk/go/dashboard"
	"github.com/grafana/grafana-foundation-sdk/go/prometheus"
	"github.com/grafana/grafana-foundation-sdk/go/stat"
	"github.com/grafana/grafana-foundation-sdk/go/timeseries"
)

func OperationsDashboard() *dashboard.DashboardBuilder {
	builder := dashboard.NewDashboardBuilder("CloudCost Exporter Operations Dashboard").
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
			// TODO: this query is grafana-specific. We should not expect every user to have `tanka_environment_info` metric.
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
		WithPanel(collectorStatusCurrent().Height(6).Span(12)).
		WithPanel(collectorScrapeDurationOverTime().Height(8).Span(24)).
		WithRow(dashboard.NewRowBuilder("AWS")).
		WithPanel(costExplorerAPIRequestsOverTime().Height(6).Span(12)).
		WithPanel(awsS3NextPricingMapRefreshOverTime().Height(6).Span(12)).
		WithRow(dashboard.NewRowBuilder("GCP")).
		WithPanel(gcpListBucketsRPSOverTime().Height(6).Span(12)).
		WithPanel(gcpNextScrapeOverTime().Height(6).Span(12))
	return builder
}

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

func collectorScrapeDurationOverTime() *timeseries.PanelBuilder {
	return timeseries.NewPanelBuilder().
		Title("Collector Scrape Duration").
		Description("Duration of scrapes by provider and collector").
		Datasource(prometheusDatasourceRef()).
		WithTarget(
			prometheusQuery("cloudcost_exporter_collector_last_scrape_duration_seconds", "{{provider}}:{{collector}}"),
		).
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
			DisplayMode(common.LegendDisplayModeTable).
			Placement(common.LegendPlacementBottom).
			ShowLegend(true).
			SortBy("Last *").
			SortDesc(true).
			Calcs([]string{"lastNotNull",
				"min",
				"max"}),
		)
}

func costExplorerAPIRequestsOverTime() *timeseries.PanelBuilder {
	return timeseries.NewPanelBuilder().
		Title("CostExplorer API Requests").
		Datasource(prometheusDatasourceRef()).
		WithTarget(
			prometheusQuery(
				"sum by (cluster) (increase(cloudcost_exporter_aws_s3_cost_api_requests_total{cluster=~\"$cluster\"}[5m]))",
				"__auto",
			),
		).
		Unit("reqps")
}

func awsS3NextPricingMapRefreshOverTime() *timeseries.PanelBuilder {
	return timeseries.NewPanelBuilder().
		Title("Next pricing map refresh").
		Description("The AWS s3 module uses cost data pulled from Cost Explorer, which costs $0.01 per API call. The cost metrics are refreshed every hour, so if this value goes below 0, it indicates a problem with refreshing the pricing map and thus needs investigation.").
		Datasource(prometheusDatasourceRef()).
		Unit("s").
		WithTarget(
			prometheusQuery(
				"max by (cluster) (cloudcost_exporter_aws_s3_next_scrape{cluster=~\"$cluster\"}) - time() ",
				"__auto",
			),
		)
}

func gcpListBucketsRPSOverTime() *timeseries.PanelBuilder {
	return timeseries.NewPanelBuilder().
		Title("GCS List Buckets Requests Per Second").
		Description("The number of requests per second to list buckets in GCS.").
		Datasource(prometheusDatasourceRef()).
		Unit("reqps").
		WithTarget(prometheusQuery("sum by (cluster, status) (increase(cloudcost_exporter_gcp_gcs_bucket_list_status_total[5m]))", "{{cluster}}:{{status}}"))
}

func gcpNextScrapeOverTime() *timeseries.PanelBuilder {
	return timeseries.NewPanelBuilder().
		Title("GCS Pricing Map Refresh Time").
		Description("The amount of time before the next refresh of the GCS pricing map. ").
		Datasource(prometheusDatasourceRef()).
		Unit("s").
		WithTarget(
			prometheusQuery("max by (cluster) (cloudcost_exporter_gcp_gcs_next_scrape{cluster=~\"$cluster\"}) - time() ", "__auto"),
		)
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
