package dashboards

import (
	"github.com/grafana/grafana-foundation-sdk/go/barchart"
	"github.com/grafana/grafana-foundation-sdk/go/cog"
	"github.com/grafana/grafana-foundation-sdk/go/cog/variants"
	"github.com/grafana/grafana-foundation-sdk/go/common"
	"github.com/grafana/grafana-foundation-sdk/go/dashboard"
	"github.com/grafana/grafana-foundation-sdk/go/logs"
	"github.com/grafana/grafana-foundation-sdk/go/loki"
	"github.com/grafana/grafana-foundation-sdk/go/prometheus"
	"github.com/grafana/grafana-foundation-sdk/go/stat"
	"github.com/grafana/grafana-foundation-sdk/go/statetimeline"
	"github.com/grafana/grafana-foundation-sdk/go/timeseries"
)

var (
	promDS = common.DataSourceRef{Type: cog.ToPtr[string]("prometheus"), Uid: cog.ToPtr[string]("${datasource}")}
	lokiDS = common.DataSourceRef{Type: cog.ToPtr[string]("loki"), Uid: cog.ToPtr[string]("${loki_datasource}")}
)

// Main entry point to generate the dashboard JSON, by calling builder.Build()
func OperationsDashboard() *dashboard.DashboardBuilder {
	builder := dashboard.NewDashboardBuilder("CloudCost Exporter operational dashboard").
		Id(3826820004253696).
		Uid("lel4qt7").
		Title("CCE operational dashboard").
		Timezone("utc").
		Editable().
		Tooltip(0).
		Timepicker(dashboard.NewTimePickerBuilder().
			RefreshIntervals([]string{"5s",
				"10s",
				"30s",
				"1m",
				"5m",
				"15m",
				"30m",
				"1h",
				"2h",
				"1d"})).
		LiveNow(false).
		Variables([]cog.Builder[dashboard.VariableModel]{
			dashboard.NewDatasourceVariableBuilder("datasource").
				Name("datasource").
				Type("prometheus").
				Label("Datasource").
				Hide(0),
			dashboard.NewDatasourceVariableBuilder("loki_datasource").
				Name("loki_datasource").
				Type("loki").
				Label("Loki Datasource").
				Hide(0),
			dashboard.NewQueryVariableBuilder("cluster").
				Name("cluster").
				Label("Cluster").
				Hide(0).
				Query(dashboard.StringOrMap{Map: map[string]interface{}{"label": "cluster", "metric": "cloudcost_exporter_collector_duration_seconds", "qryType": 1, "query": "label_values(cloudcost_exporter_collector_duration_seconds,cluster)", "refId": "PrometheusVariableQueryEditor-VariableQuery"}}).
				Datasource(promDS).
				Current(dashboard.VariableOption{Text: dashboard.StringOrArrayOfString{String: cog.ToPtr[string]("All")}, Value: dashboard.StringOrArrayOfString{ArrayOfString: []string{"$__all"}}}).
				Multi(true).
				Refresh(1).
				Sort(1).
				IncludeAll(true),
			dashboard.NewQueryVariableBuilder("collector").
				Name("collector").
				Label("Collector").
				Hide(0).
				Query(dashboard.StringOrMap{Map: map[string]interface{}{"refId": "PrometheusVariableQueryEditor-VariableQuery", "label": "collector", "metric": "cloudcost_exporter_collector_duration_seconds", "qryType": 1, "query": "label_values(cloudcost_exporter_collector_duration_seconds,collector)"}}).
				Datasource(promDS).
				Current(dashboard.VariableOption{Text: dashboard.StringOrArrayOfString{String: cog.ToPtr[string]("All")}, Value: dashboard.StringOrArrayOfString{ArrayOfString: []string{"$__all"}}}).
				Multi(true).
				Refresh(1).
				Sort(1).
				IncludeAll(true)}).
		Annotations([]cog.Builder[dashboard.AnnotationQuery]{dashboard.NewAnnotationQueryBuilder().
			Name("Annotations & Alerts").
			Datasource(common.DataSourceRef{Type: cog.ToPtr[string]("grafana"), Uid: cog.ToPtr[string]("-- Grafana --")}).
			Hide(true).
			IconColor("rgba(0, 211, 255, 1)").
			Type("dashboard").
			BuiltIn(1)}).
		Preload(false).
		WithRow(dashboard.NewRowBuilder("Overview").
			Title("Overview").
			GridPos(dashboard.GridPos{H: 1, W: 24, X: 0, Y: 1}).
			Id(0x48)).
		WithPanel(stat.NewPanelBuilder().
			Id(0x11).
			Targets([]cog.Builder[variants.Dataquery]{prometheus.NewDataqueryBuilder().
				Expr("count(count by(collector) (rate(cloudcost_exporter_collector_duration_seconds{cluster=~\"$cluster\", collector=~\"$collector\"}[$__rate_interval])))").
				Instant().
				LegendFormat("Active").
				RefId("A").
				QueryType("instant").
				Datasource(promDS),
				prometheus.NewDataqueryBuilder().
					Expr("count(count by(collector) (count_over_time(cloudcost_exporter_collector_duration_seconds{cluster=~\"$cluster\", collector=~\"$collector\"}[$__range])))").
					Instant().
					EditorMode("code").
					LegendFormat("Expected Active").
					RefId("B").
					QueryType("instant").
					Datasource(promDS)}).
			Title("Active Collectors").
			Description("Active: distinct collectors currently reporting duration metrics. Expected Active: distinct collectors seen at any point in the selected time range.").
			Datasource(promDS).
			GridPos(dashboard.GridPos{H: 9, W: 8, X: 0, Y: 1}).
			Span(0x8).
			Unit("short").
			Thresholds(dashboard.NewThresholdsConfigBuilder().
				Mode("absolute").
				Steps([]dashboard.Threshold{dashboard.Threshold{Value: cog.ToPtr[float64](0), Color: "green"},
					dashboard.Threshold{Value: cog.ToPtr[float64](80), Color: "red"}})).
			ColorScheme(dashboard.NewFieldColorBuilder().
				Mode("thresholds").
				FixedColor("#ad46ff")).
			GraphMode("none").
			ColorMode("value").
			JustifyMode("auto").
			TextMode("auto").
			ReduceOptions(common.NewReduceDataOptionsBuilder().
				Values(false).
				Calcs([]string{"lastNotNull"})).
			PercentChangeColorMode("standard").
			Orientation("horizontal")).
		WithPanel(stat.NewPanelBuilder().
			Id(0x12).
			Targets([]cog.Builder[variants.Dataquery]{prometheus.NewDataqueryBuilder().
				Expr("histogram_quantile(0.95, sum(rate(cloudcost_exporter_collector_duration_seconds{cluster=~\"$cluster\", collector=~\"$collector\"}[$__rate_interval])))").
				Instant().
				LegendFormat("P95").
				RefId("A").
				QueryType("instant").
				Datasource(promDS)}).
			Title("P95 Collection Duration").
			Description("95th percentile of collector run duration across all selected clusters and collectors.").
			Datasource(promDS).
			GridPos(dashboard.GridPos{H: 9, W: 8, X: 8, Y: 1}).
			Span(0x8).
			Unit("s").
			Thresholds(dashboard.NewThresholdsConfigBuilder().
				Mode("absolute").
				Steps([]dashboard.Threshold{dashboard.Threshold{Value: cog.ToPtr[float64](0), Color: "green"},
					dashboard.Threshold{Value: cog.ToPtr[float64](30), Color: "yellow"},
					dashboard.Threshold{Value: cog.ToPtr[float64](120), Color: "red"}})).
			ColorScheme(dashboard.NewFieldColorBuilder().
				Mode("thresholds")).
			GraphMode("none").
			ColorMode("background").
			JustifyMode("auto").
			TextMode("auto").
			ReduceOptions(common.NewReduceDataOptionsBuilder().
				Values(false).
				Calcs([]string{"lastNotNull"})).
			PercentChangeColorMode("standard").
			Orientation("auto")).
		WithPanel(stat.NewPanelBuilder().
			Id(0x2d).
			Targets([]cog.Builder[variants.Dataquery]{prometheus.NewDataqueryBuilder().
				Expr("(1 - (sum(rate(cloudcost_exporter_collector_error{cluster=~\"$cluster\", collector=~\"$collector\"}[$__rate_interval])) or vector(0)) / sum(rate(cloudcost_exporter_collector_total{cluster=~\"$cluster\", collector=~\"$collector\"}[$__rate_interval]))) * 100").
				Instant().
				EditorMode("code").
				LegendFormat("Success Rate").
				RefId("A").
				QueryType("instant").
				Datasource(promDS)}).
			Title("Success Rate").
			Description("Percentage of successful collector runs (total - errors) / total.").
			Datasource(promDS).
			GridPos(dashboard.GridPos{H: 9, W: 8, X: 16, Y: 1}).
			Span(0x8).
			Unit("percent").
			Min(0).
			Max(100).
			Thresholds(dashboard.NewThresholdsConfigBuilder().
				Mode("absolute").
				Steps([]dashboard.Threshold{dashboard.Threshold{Value: cog.ToPtr[float64](0), Color: "red"},
					dashboard.Threshold{Value: cog.ToPtr[float64](90), Color: "yellow"},
					dashboard.Threshold{Value: cog.ToPtr[float64](95), Color: "green"}})).
			ColorScheme(dashboard.NewFieldColorBuilder().
				Mode("thresholds")).
			GraphMode("area").
			ColorMode("background").
			JustifyMode("auto").
			TextMode("auto").
			ReduceOptions(common.NewReduceDataOptionsBuilder().
				Values(false).
				Calcs([]string{"lastNotNull"})).
			PercentChangeColorMode("standard").
			Orientation("auto")).
		WithPanel(timeseries.NewPanelBuilder().
			Id(0x31).
			Targets([]cog.Builder[variants.Dataquery]{prometheus.NewDataqueryBuilder().
				Expr("((sum by(collector) (rate(cloudcost_exporter_collector_error{cluster=~\"$cluster\", collector=~\"$collector\"}[$__rate_interval])) or (0 * sum by(collector) (rate(cloudcost_exporter_collector_total{cluster=~\"$cluster\", collector=~\"$collector\"}[$__rate_interval])))) / sum by(collector) (rate(cloudcost_exporter_collector_total{cluster=~\"$cluster\", collector=~\"$collector\"}[$__rate_interval]))) * 100").
				Range().
				LegendFormat("{{collector}}").
				RefId("A").
				QueryType("range").
				Datasource(promDS)}).
			Title("Error Rate by Collector (%)").
			Description("Per-collector error rate over time — errors / total collector runs.").
			Datasource(promDS).
			GridPos(dashboard.GridPos{H: 9, W: 8, X: 0, Y: 10}).
			Span(0x8).
			Unit("percent").
			Min(0).
			Thresholds(dashboard.NewThresholdsConfigBuilder().
				Mode("absolute").
				Steps([]dashboard.Threshold{dashboard.Threshold{Value: cog.ToPtr[float64](0), Color: "green"}})).
			ColorScheme(dashboard.NewFieldColorBuilder().
				Mode("palette-classic")).
			Legend(common.NewVizLegendOptionsBuilder().
				DisplayMode("table").
				Placement("right").
				ShowLegend(true).
				Calcs([]string{"mean",
					"max",
					"lastNotNull"})).
			Tooltip(common.NewVizTooltipOptionsBuilder().
				Mode("multi").
				Sort("desc").
				HideZeros(false)).
			DrawStyle("line").
			GradientMode("none").
			ThresholdsStyle(common.NewGraphThresholdsStyleConfigBuilder().
				Mode("off")).
			LineWidth(2).
			LineInterpolation("smooth").
			FillOpacity(10).
			ShowPoints("never").
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
			AxisBorderShow(false)).
		WithPanel(timeseries.NewPanelBuilder().
			Id(0x32).
			Targets([]cog.Builder[variants.Dataquery]{prometheus.NewDataqueryBuilder().
				Expr("(1 - ((sum by(collector) (rate(cloudcost_exporter_collector_error{cluster=~\"$cluster\", collector=~\"$collector\"}[$__rate_interval])) or (0 * sum by(collector) (rate(cloudcost_exporter_collector_total{cluster=~\"$cluster\", collector=~\"$collector\"}[$__rate_interval])))) / sum by(collector) (rate(cloudcost_exporter_collector_total{cluster=~\"$cluster\", collector=~\"$collector\"}[$__rate_interval])))) * 100").
				Range().
				LegendFormat("{{collector}}").
				RefId("A").
				QueryType("range").
				Datasource(promDS)}).
			Title("Success Rate by Collector (%)").
			Description("Per-collector success rate over time.").
			Datasource(promDS).
			GridPos(dashboard.GridPos{H: 9, W: 8, X: 8, Y: 10}).
			Span(0x8).
			Unit("percent").
			Min(0).
			Max(100).
			Thresholds(dashboard.NewThresholdsConfigBuilder().
				Mode("absolute").
				Steps([]dashboard.Threshold{dashboard.Threshold{Value: cog.ToPtr[float64](0), Color: "green"}})).
			ColorScheme(dashboard.NewFieldColorBuilder().
				Mode("palette-classic")).
			Legend(common.NewVizLegendOptionsBuilder().
				DisplayMode("table").
				Placement("right").
				ShowLegend(true).
				Calcs([]string{"mean",
					"min",
					"lastNotNull"})).
			Tooltip(common.NewVizTooltipOptionsBuilder().
				Mode("multi").
				Sort("asc").
				HideZeros(false)).
			DrawStyle("line").
			GradientMode("none").
			ThresholdsStyle(common.NewGraphThresholdsStyleConfigBuilder().
				Mode("off")).
			LineWidth(2).
			LineInterpolation("smooth").
			FillOpacity(5).
			ShowPoints("never").
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
			AxisBorderShow(false)).
		WithPanel(timeseries.NewPanelBuilder().
			Id(0x33).
			Targets([]cog.Builder[variants.Dataquery]{prometheus.NewDataqueryBuilder().
				Expr("sum(rate(cloudcost_exporter_collector_total{cluster=~\"$cluster\", collector=~\"$collector\"}[$__rate_interval]))").
				Range().
				LegendFormat("Total/s").
				RefId("A").
				QueryType("range").
				Datasource(promDS),
				prometheus.NewDataqueryBuilder().
					Expr("sum(rate(cloudcost_exporter_collector_error{cluster=~\"$cluster\", collector=~\"$collector\"}[$__rate_interval])) or vector(0)").
					Range().
					LegendFormat("Errors/s").
					RefId("B").
					QueryType("range").
					Datasource(promDS)}).
			Title("Collection Rate vs Error Rate").
			Description("Absolute collection rate (Total/s) and error rate (Errors/s) over time.").
			Datasource(promDS).
			GridPos(dashboard.GridPos{H: 9, W: 8, X: 16, Y: 10}).
			Span(0x8).
			Unit("reqps").
			Thresholds(dashboard.NewThresholdsConfigBuilder().
				Mode("absolute").
				Steps([]dashboard.Threshold{dashboard.Threshold{Value: cog.ToPtr[float64](0), Color: "green"}})).
			ColorScheme(dashboard.NewFieldColorBuilder().
				Mode("palette-classic")).
			Overrides([]cog.Builder[dashboard.DashboardFieldConfigSourceOverrides]{dashboard.NewDashboardFieldConfigSourceOverridesBuilder().
				Matcher(dashboard.MatcherConfig{Id: "byName", Options: "Errors/s"}).
				Properties([]dashboard.DynamicConfigValue{dashboard.DynamicConfigValue{Id: "color", Value: map[string]interface{}{"fixedColor": "red", "mode": "fixed"}}})}).
			WithOverride(dashboard.MatcherConfig{Id: "byName", Options: "Errors/s"}, []dashboard.DynamicConfigValue{dashboard.DynamicConfigValue{Id: "color", Value: map[string]interface{}{"fixedColor": "red", "mode": "fixed"}}}).
			Legend(common.NewVizLegendOptionsBuilder().
				DisplayMode("table").
				Placement("right").
				ShowLegend(true).
				Calcs([]string{"mean",
					"max"})).
			Tooltip(common.NewVizTooltipOptionsBuilder().
				Mode("multi").
				Sort("desc").
				HideZeros(false)).
			DrawStyle("line").
			GradientMode("none").
			ThresholdsStyle(common.NewGraphThresholdsStyleConfigBuilder().
				Mode("off")).
			LineWidth(2).
			LineInterpolation("smooth").
			FillOpacity(10).
			ShowPoints("never").
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
			AxisBorderShow(false)).
		WithPanel(statetimeline.NewPanelBuilder().
			Id(0x46).
			Targets([]cog.Builder[variants.Dataquery]{prometheus.NewDataqueryBuilder().
				Expr("topk without(version) (1, max by(cluster, version) (cloudcost_exporter_build_info)) and on(version) topk(1, count by(version) (max by(cluster, version) (cloudcost_exporter_build_info)))").
				Range().
				LegendFormat("{{cluster}} - {{version}}").
				RefId("A").
				QueryType("range").
				Datasource(promDS),
				prometheus.NewDataqueryBuilder().
					Expr("(topk without(version) (1, max by(cluster, version) (cloudcost_exporter_build_info)) unless on(version) topk(1, count by(version) (max by(cluster, version) (cloudcost_exporter_build_info)))) * 0").
					Range().
					LegendFormat("{{cluster}} - {{version}}").
					RefId("B").
					QueryType("range").
					Datasource(promDS)}).
			Title("Version History per Cluster").
			Description("Shows when each version became active per cluster. Transitions indicate upgrade events.").
			Datasource(promDS).
			GridPos(dashboard.GridPos{H: 9, W: 24, X: 0, Y: 19}).
			Span(0x18).
			Thresholds(dashboard.NewThresholdsConfigBuilder().
				Mode("absolute").
				Steps([]dashboard.Threshold{dashboard.Threshold{Value: cog.ToPtr[float64](0), Color: "orange"},
					dashboard.Threshold{Value: cog.ToPtr[float64](1), Color: "green"}})).
			ColorScheme(dashboard.NewFieldColorBuilder().
				Mode("thresholds")).
			ShowValue("never").
			RowHeight(0.8).
			AlignValue("left").
			Legend(common.NewVizLegendOptionsBuilder().
				DisplayMode("list").
				Placement("bottom").
				ShowLegend(false)).
			Tooltip(common.NewVizTooltipOptionsBuilder().
				Mode("single").
				Sort("none").
				HideZeros(false)).
			AxisPlacement("auto").
			HideFrom(common.NewHideSeriesConfigBuilder().
				Tooltip(false).
				Legend(false).
				Viz(false))).
		WithRow(dashboard.NewRowBuilder("By Provider").
			Title("By Provider").
			GridPos(dashboard.GridPos{H: 1, W: 24, X: 0, Y: 28}).
			Id(0x49)).
		WithPanel(stat.NewPanelBuilder().
			Id(0x40).
			Targets([]cog.Builder[variants.Dataquery]{prometheus.NewDataqueryBuilder().
				Expr("histogram_quantile(0.95, sum(rate(cloudcost_exporter_collector_duration_seconds{cluster=~\"$cluster\", collector=~\"aws_ec2|aws_elb|aws_rds|S3|NATGATEWAY|VPC\"}[$__rate_interval])))").
				Instant().
				LegendFormat("AWS").
				RefId("A").
				QueryType("instant").
				Datasource(promDS)}).
			Title("P95 Duration — AWS").
			Description("P95 collection duration across AWS collectors (aws_ec2, aws_elb, aws_rds, S3, NATGATEWAY, VPC). Unaffected by the collector variable.").
			Datasource(promDS).
			GridPos(dashboard.GridPos{H: 6, W: 8, X: 0, Y: 28}).
			Height(0x6).
			Span(0x8).
			Unit("s").
			Thresholds(dashboard.NewThresholdsConfigBuilder().
				Mode("absolute").
				Steps([]dashboard.Threshold{dashboard.Threshold{Value: cog.ToPtr[float64](0), Color: "green"},
					dashboard.Threshold{Value: cog.ToPtr[float64](30), Color: "yellow"},
					dashboard.Threshold{Value: cog.ToPtr[float64](120), Color: "red"}})).
			ColorScheme(dashboard.NewFieldColorBuilder().
				Mode("thresholds")).
			GraphMode("none").
			ColorMode("background").
			JustifyMode("auto").
			TextMode("auto").
			ReduceOptions(common.NewReduceDataOptionsBuilder().
				Values(false).
				Calcs([]string{"lastNotNull"})).
			PercentChangeColorMode("standard").
			Orientation("auto")).
		WithPanel(stat.NewPanelBuilder().
			Id(0x41).
			Targets([]cog.Builder[variants.Dataquery]{prometheus.NewDataqueryBuilder().
				Expr("histogram_quantile(0.95, sum(rate(cloudcost_exporter_collector_duration_seconds{cluster=~\"$cluster\", collector=~\"gcp_gke|GCS|ForwardingRule\"}[$__rate_interval])))").
				Instant().
				LegendFormat("GCP").
				RefId("A").
				QueryType("instant").
				Datasource(promDS)}).
			Title("P95 Duration — GCP").
			Description("P95 collection duration across GCP collectors (gcp_gke, GCS, ForwardingRule). Unaffected by the collector variable.").
			Datasource(promDS).
			GridPos(dashboard.GridPos{H: 6, W: 8, X: 8, Y: 28}).
			Height(0x6).
			Span(0x8).
			Unit("s").
			Thresholds(dashboard.NewThresholdsConfigBuilder().
				Mode("absolute").
				Steps([]dashboard.Threshold{dashboard.Threshold{Value: cog.ToPtr[float64](0), Color: "green"},
					dashboard.Threshold{Value: cog.ToPtr[float64](30), Color: "yellow"},
					dashboard.Threshold{Value: cog.ToPtr[float64](120), Color: "red"}})).
			ColorScheme(dashboard.NewFieldColorBuilder().
				Mode("thresholds")).
			GraphMode("none").
			ColorMode("background").
			JustifyMode("auto").
			TextMode("auto").
			ReduceOptions(common.NewReduceDataOptionsBuilder().
				Values(false).
				Calcs([]string{"lastNotNull"})).
			PercentChangeColorMode("standard").
			Orientation("auto")).
		WithPanel(stat.NewPanelBuilder().
			Id(0x42).
			Targets([]cog.Builder[variants.Dataquery]{prometheus.NewDataqueryBuilder().
				Expr("histogram_quantile(0.95, sum(rate(cloudcost_exporter_collector_duration_seconds{cluster=~\"$cluster\", collector=~\"azure_aks\"}[$__rate_interval])))").
				Instant().
				LegendFormat("Azure").
				RefId("A").
				QueryType("instant").
				Datasource(promDS)}).
			Title("P95 Duration — Azure").
			Description("P95 collection duration across Azure collectors (azure_aks). Unaffected by the collector variable.").
			Datasource(promDS).
			GridPos(dashboard.GridPos{H: 6, W: 8, X: 16, Y: 28}).
			Height(0x6).
			Span(0x8).
			Unit("s").
			Thresholds(dashboard.NewThresholdsConfigBuilder().
				Mode("absolute").
				Steps([]dashboard.Threshold{dashboard.Threshold{Value: cog.ToPtr[float64](0), Color: "green"},
					dashboard.Threshold{Value: cog.ToPtr[float64](30), Color: "yellow"},
					dashboard.Threshold{Value: cog.ToPtr[float64](120), Color: "red"}})).
			ColorScheme(dashboard.NewFieldColorBuilder().
				Mode("thresholds")).
			GraphMode("none").
			ColorMode("background").
			JustifyMode("auto").
			TextMode("auto").
			ReduceOptions(common.NewReduceDataOptionsBuilder().
				Values(false).
				Calcs([]string{"lastNotNull"})).
			PercentChangeColorMode("standard").
			Orientation("auto")).
		WithPanel(stat.NewPanelBuilder().
			Id(0x43).
			Targets([]cog.Builder[variants.Dataquery]{prometheus.NewDataqueryBuilder().
				Expr("(1 - (sum(rate(cloudcost_exporter_collector_error{cluster=~\"$cluster\", collector=~\"aws_ec2|aws_elb|aws_rds|S3|NATGATEWAY|VPC\"}[$__rate_interval])) or vector(0)) / sum(rate(cloudcost_exporter_collector_total{cluster=~\"$cluster\", collector=~\"aws_ec2|aws_elb|aws_rds|S3|NATGATEWAY|VPC\"}[$__rate_interval]))) * 100").
				Instant().
				LegendFormat("AWS").
				RefId("A").
				QueryType("instant").
				Datasource(promDS)}).
			Title("Success Rate — AWS").
			Description("Success rate for AWS collectors (aws_ec2, aws_elb, aws_rds, S3, NATGATEWAY, VPC). Unaffected by the collector variable. Falls back to 0 errors when no error series are present.").
			Datasource(promDS).
			GridPos(dashboard.GridPos{H: 6, W: 8, X: 0, Y: 34}).
			Height(0x6).
			Span(0x8).
			Unit("percent").
			Min(0).
			Max(100).
			Thresholds(dashboard.NewThresholdsConfigBuilder().
				Mode("absolute").
				Steps([]dashboard.Threshold{dashboard.Threshold{Value: cog.ToPtr[float64](0), Color: "red"},
					dashboard.Threshold{Value: cog.ToPtr[float64](90), Color: "yellow"},
					dashboard.Threshold{Value: cog.ToPtr[float64](95), Color: "green"}})).
			ColorScheme(dashboard.NewFieldColorBuilder().
				Mode("thresholds")).
			GraphMode("none").
			ColorMode("background").
			JustifyMode("auto").
			TextMode("auto").
			ReduceOptions(common.NewReduceDataOptionsBuilder().
				Values(false).
				Calcs([]string{"lastNotNull"})).
			PercentChangeColorMode("standard").
			Orientation("auto")).
		WithPanel(stat.NewPanelBuilder().
			Id(0x44).
			Targets([]cog.Builder[variants.Dataquery]{prometheus.NewDataqueryBuilder().
				Expr("(1 - (sum(rate(cloudcost_exporter_collector_error{cluster=~\"$cluster\", collector=~\"gcp_gke|GCS|ForwardingRule\"}[$__rate_interval])) or vector(0)) / sum(rate(cloudcost_exporter_collector_total{cluster=~\"$cluster\", collector=~\"gcp_gke|GCS|ForwardingRule\"}[$__rate_interval]))) * 100").
				Instant().
				LegendFormat("GCP").
				RefId("A").
				QueryType("instant").
				Datasource(promDS)}).
			Title("Success Rate — GCP").
			Description("Success rate for GCP collectors (gcp_gke, GCS, ForwardingRule). Unaffected by the collector variable. Falls back to 0 errors when no error series are present.").
			Datasource(promDS).
			GridPos(dashboard.GridPos{H: 6, W: 8, X: 8, Y: 34}).
			Height(0x6).
			Span(0x8).
			Unit("percent").
			Min(0).
			Max(100).
			Thresholds(dashboard.NewThresholdsConfigBuilder().
				Mode("absolute").
				Steps([]dashboard.Threshold{dashboard.Threshold{Value: cog.ToPtr[float64](0), Color: "red"},
					dashboard.Threshold{Value: cog.ToPtr[float64](90), Color: "yellow"},
					dashboard.Threshold{Value: cog.ToPtr[float64](95), Color: "green"}})).
			ColorScheme(dashboard.NewFieldColorBuilder().
				Mode("thresholds")).
			GraphMode("none").
			ColorMode("background").
			JustifyMode("auto").
			TextMode("auto").
			ReduceOptions(common.NewReduceDataOptionsBuilder().
				Values(false).
				Calcs([]string{"lastNotNull"})).
			PercentChangeColorMode("standard").
			Orientation("auto")).
		WithPanel(stat.NewPanelBuilder().
			Id(0x45).
			Targets([]cog.Builder[variants.Dataquery]{prometheus.NewDataqueryBuilder().
				Expr("(1 - (sum(rate(cloudcost_exporter_collector_error{cluster=~\"$cluster\", collector=~\"azure_aks\"}[$__rate_interval])) or vector(0)) / sum(rate(cloudcost_exporter_collector_total{cluster=~\"$cluster\", collector=~\"azure_aks\"}[$__rate_interval]))) * 100").
				Instant().
				LegendFormat("Azure").
				RefId("A").
				QueryType("instant").
				Datasource(promDS)}).
			Title("Success Rate — Azure").
			Description("Success rate for Azure collectors (azure_aks). Unaffected by the collector variable. Falls back to 0 errors when no error series are present.").
			Datasource(promDS).
			GridPos(dashboard.GridPos{H: 6, W: 8, X: 16, Y: 34}).
			Height(0x6).
			Span(0x8).
			Unit("percent").
			Min(0).
			Max(100).
			Thresholds(dashboard.NewThresholdsConfigBuilder().
				Mode("absolute").
				Steps([]dashboard.Threshold{dashboard.Threshold{Value: cog.ToPtr[float64](0), Color: "red"},
					dashboard.Threshold{Value: cog.ToPtr[float64](90), Color: "yellow"},
					dashboard.Threshold{Value: cog.ToPtr[float64](95), Color: "green"}})).
			ColorScheme(dashboard.NewFieldColorBuilder().
				Mode("thresholds")).
			GraphMode("none").
			ColorMode("background").
			JustifyMode("auto").
			TextMode("auto").
			ReduceOptions(common.NewReduceDataOptionsBuilder().
				Values(false).
				Calcs([]string{"lastNotNull"})).
			PercentChangeColorMode("standard").
			Orientation("auto")).
		WithRow(dashboard.NewRowBuilder("Logs").
			Title("Logs").
			GridPos(dashboard.GridPos{H: 1, W: 24, X: 0, Y: 40}).
			Id(0x4a)).
		WithPanel(timeseries.NewPanelBuilder().
			Id(0x37).
			Targets([]cog.Builder[variants.Dataquery]{loki.NewDataqueryBuilder().
				Expr("sum(rate({namespace=\"cloudcost-exporter\", container=\"cloudcost-exporter\", cluster=~\"$cluster\"} |~ \"(?i)(level=error|\\\"level\\\":\\\"error\\\"|\\\"level\\\":\\\"ERROR\\\"|level=\\\"error\\\")\" [$__auto]))").
				LegendFormat("Error rate").
				Range(true).
				Instant(false).
				RefId("A").
				QueryType("range").
				Datasource(lokiDS)}).
			Title("Error Log Rate").
			Description("Rate of log lines containing error-level messages from the cloudcost-exporter namespace.").
			Datasource(lokiDS).
			GridPos(dashboard.GridPos{H: 8, W: 24, X: 0, Y: 40}).
			Height(0x8).
			Span(0x18).
			Unit("logrowspersec").
			Thresholds(dashboard.NewThresholdsConfigBuilder().
				Mode("absolute").
				Steps([]dashboard.Threshold{dashboard.Threshold{Value: cog.ToPtr[float64](0), Color: "green"}})).
			ColorScheme(dashboard.NewFieldColorBuilder().
				Mode("fixed").
				FixedColor("red")).
			Legend(common.NewVizLegendOptionsBuilder().
				DisplayMode("hidden").
				Placement("bottom").
				ShowLegend(false)).
			Tooltip(common.NewVizTooltipOptionsBuilder().
				Mode("single").
				Sort("none").
				HideZeros(false)).
			DrawStyle("bars").
			GradientMode("none").
			ThresholdsStyle(common.NewGraphThresholdsStyleConfigBuilder().
				Mode("off")).
			LineWidth(1).
			LineInterpolation("linear").
			FillOpacity(60).
			ShowPoints("never").
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
			AxisBorderShow(false)).
		WithPanel(logs.NewPanelBuilder().
			Id(0x38).
			Targets([]cog.Builder[variants.Dataquery]{loki.NewDataqueryBuilder().
				Expr("{namespace=\"cloudcost-exporter\", container=\"cloudcost-exporter\", cluster=~\"$cluster\"} |~ \"(?i)(level=error|\\\"level\\\":\\\"error\\\"|\\\"level\\\":\\\"ERROR\\\"|level=\\\"error\\\")\"").
				Range(true).
				Instant(false).
				RefId("A").
				QueryType("range").
				Datasource(lokiDS)}).
			Title("Error Logs").
			Description("Log lines at error level from the cloudcost-exporter namespace.").
			Datasource(lokiDS).
			GridPos(dashboard.GridPos{H: 10, W: 24, X: 0, Y: 48}).
			Height(0xa).
			Span(0x18).
			ShowLabels(false).
			ShowCommonLabels(false).
			ShowTime(true).
			ShowLogContextToggle(false).
			WrapLogMessage(false).
			PrettifyLogMessage(true).
			EnableLogDetails(true).
			SortOrder("Descending").
			DedupStrategy("none").
			EnableInfiniteScrolling(false)).
		WithPanel(logs.NewPanelBuilder().
			Id(0x39).
			Targets([]cog.Builder[variants.Dataquery]{loki.NewDataqueryBuilder().
				Expr("{namespace=\"cloudcost-exporter\", container=\"cloudcost-exporter\", cluster=~\"$cluster\"}").
				Range(true).
				Instant(false).
				RefId("A").
				QueryType("range").
				Datasource(lokiDS)}).
			Title("All Logs (cloudcost-exporter namespace)").
			Description("All logs from the cloudcost-exporter namespace across selected clusters.").
			Datasource(lokiDS).
			GridPos(dashboard.GridPos{H: 10, W: 24, X: 0, Y: 58}).
			Height(0xa).
			Span(0x18).
			ShowLabels(false).
			ShowCommonLabels(false).
			ShowTime(true).
			ShowLogContextToggle(false).
			WrapLogMessage(false).
			PrettifyLogMessage(true).
			EnableLogDetails(true).
			SortOrder("Descending").
			DedupStrategy("none").
			EnableInfiniteScrolling(false)).
		WithRow(dashboard.NewRowBuilder("By Collector").
			Title("By Collector").
			GridPos(dashboard.GridPos{H: 1, W: 24, X: 0, Y: 69}).
			Id(0x4c)).
		WithPanel(timeseries.NewPanelBuilder().
			Id(0x19).
			Targets([]cog.Builder[variants.Dataquery]{prometheus.NewDataqueryBuilder().
				Expr("histogram_quantile(0.99, sum by(collector) (rate(cloudcost_exporter_collector_duration_seconds{cluster=~\"$cluster\", collector=~\"$collector\"}[$__rate_interval])))").
				Range().
				LegendFormat("{{collector}}").
				RefId("A").
				QueryType("range").
				Datasource(promDS)}).
			Title("P99 Duration by Collector").
			Description("P99 collection duration trend per collector.").
			Datasource(promDS).
			GridPos(dashboard.GridPos{H: 9, W: 8, X: 0, Y: 69}).
			Span(0x8).
			Unit("s").
			Thresholds(dashboard.NewThresholdsConfigBuilder().
				Mode("absolute").
				Steps([]dashboard.Threshold{dashboard.Threshold{Value: cog.ToPtr[float64](0), Color: "green"},
					dashboard.Threshold{Value: cog.ToPtr[float64](80), Color: "red"}})).
			ColorScheme(dashboard.NewFieldColorBuilder().
				Mode("palette-classic")).
			Legend(common.NewVizLegendOptionsBuilder().
				DisplayMode("table").
				Placement("right").
				ShowLegend(true).
				Calcs([]string{"mean",
					"max",
					"lastNotNull"})).
			Tooltip(common.NewVizTooltipOptionsBuilder().
				Mode("multi").
				Sort("desc").
				HideZeros(false)).
			DrawStyle("line").
			GradientMode("none").
			ThresholdsStyle(common.NewGraphThresholdsStyleConfigBuilder().
				Mode("off")).
			LineWidth(2).
			LineInterpolation("smooth").
			FillOpacity(10).
			ShowPoints("never").
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
			AxisBorderShow(false)).
		WithPanel(timeseries.NewPanelBuilder().
			Id(0x17).
			Targets([]cog.Builder[variants.Dataquery]{prometheus.NewDataqueryBuilder().
				Expr("histogram_quantile(0.95, sum by(collector) (rate(cloudcost_exporter_collector_duration_seconds{cluster=~\"$cluster\", collector=~\"$collector\"}[$__rate_interval])))").
				Range().
				LegendFormat("{{collector}}").
				RefId("A").
				QueryType("range").
				Datasource(promDS)}).
			Title("P95 Duration by Collector").
			Description("P95 collection duration trend per collector.").
			Datasource(promDS).
			GridPos(dashboard.GridPos{H: 9, W: 8, X: 8, Y: 69}).
			Span(0x8).
			Unit("s").
			Thresholds(dashboard.NewThresholdsConfigBuilder().
				Mode("absolute").
				Steps([]dashboard.Threshold{dashboard.Threshold{Value: cog.ToPtr[float64](0), Color: "green"},
					dashboard.Threshold{Value: cog.ToPtr[float64](80), Color: "red"}})).
			ColorScheme(dashboard.NewFieldColorBuilder().
				Mode("palette-classic")).
			Legend(common.NewVizLegendOptionsBuilder().
				DisplayMode("table").
				Placement("right").
				ShowLegend(true).
				Calcs([]string{"mean",
					"max"})).
			Tooltip(common.NewVizTooltipOptionsBuilder().
				Mode("multi").
				Sort("desc").
				HideZeros(false)).
			DrawStyle("line").
			GradientMode("none").
			ThresholdsStyle(common.NewGraphThresholdsStyleConfigBuilder().
				Mode("off")).
			LineWidth(2).
			LineInterpolation("smooth").
			FillOpacity(5).
			ShowPoints("never").
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
			AxisBorderShow(false)).
		WithPanel(timeseries.NewPanelBuilder().
			Id(0x18).
			Targets([]cog.Builder[variants.Dataquery]{prometheus.NewDataqueryBuilder().
				Expr("histogram_quantile(0.50, sum by(collector) (rate(cloudcost_exporter_collector_duration_seconds{cluster=~\"$cluster\", collector=~\"$collector\"}[$__rate_interval])))").
				Range().
				LegendFormat("{{collector}}").
				RefId("A").
				QueryType("range").
				Datasource(promDS)}).
			Title("P50 Duration by Collector").
			Description("P50 (median) collection duration trend per collector.").
			Datasource(promDS).
			GridPos(dashboard.GridPos{H: 9, W: 8, X: 16, Y: 69}).
			Span(0x8).
			Unit("s").
			Thresholds(dashboard.NewThresholdsConfigBuilder().
				Mode("absolute").
				Steps([]dashboard.Threshold{dashboard.Threshold{Value: cog.ToPtr[float64](0), Color: "green"},
					dashboard.Threshold{Value: cog.ToPtr[float64](80), Color: "red"}})).
			ColorScheme(dashboard.NewFieldColorBuilder().
				Mode("palette-classic")).
			Legend(common.NewVizLegendOptionsBuilder().
				DisplayMode("table").
				Placement("right").
				ShowLegend(true).
				Calcs([]string{"mean",
					"max"})).
			Tooltip(common.NewVizTooltipOptionsBuilder().
				Mode("multi").
				Sort("desc").
				HideZeros(false)).
			DrawStyle("line").
			GradientMode("none").
			ThresholdsStyle(common.NewGraphThresholdsStyleConfigBuilder().
				Mode("off")).
			LineWidth(2).
			LineInterpolation("smooth").
			FillOpacity(5).
			ShowPoints("never").
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
			AxisBorderShow(false)).
		WithPanel(barchart.NewPanelBuilder().
			Id(0x1a).
			Targets([]cog.Builder[variants.Dataquery]{prometheus.NewDataqueryBuilder().
				Expr("sort_desc(histogram_quantile(0.95, sum by(collector) (rate(cloudcost_exporter_collector_duration_seconds{cluster=~\"$cluster\", collector=~\"$collector\"}[$__rate_interval]))))").
				Instant().
				LegendFormat("{{collector}}").
				RefId("A").
				QueryType("instant").
				Datasource(promDS)}).
			Title("P95 Duration by Collector (current, ranked)").
			Description("P95 collection duration per collector, ranked descending. Computed over the full selected time range via $__rate_interval — for narrow time ranges this reflects recent behavior; for wide ranges (e.g. 2d) it averages over the whole window.").
			Datasource(promDS).
			GridPos(dashboard.GridPos{H: 8, W: 24, X: 0, Y: 78}).
			Height(0x8).
			Span(0x18).
			Unit("s").
			Thresholds(dashboard.NewThresholdsConfigBuilder().
				Mode("absolute").
				Steps([]dashboard.Threshold{dashboard.Threshold{Value: cog.ToPtr[float64](0), Color: "green"},
					dashboard.Threshold{Value: cog.ToPtr[float64](80), Color: "red"}})).
			ColorScheme(dashboard.NewFieldColorBuilder().
				Mode("palette-classic")).
			Orientation("horizontal").
			XTickLabelMaxLength(0).
			Stacking("none").
			ShowValue("auto").
			Legend(common.NewVizLegendOptionsBuilder().
				DisplayMode("hidden").
				Placement("bottom").
				ShowLegend(false)).
			Tooltip(common.NewVizTooltipOptionsBuilder().
				Mode("single").
				Sort("none").
				HideZeros(false)).
			GradientMode("none").
			AxisPlacement("auto").
			AxisColorMode("text").
			ScaleDistribution(common.NewScaleDistributionConfigBuilder().
				Type("linear")).
			AxisCenteredZero(false).
			HideFrom(common.NewHideSeriesConfigBuilder().
				Tooltip(false).
				Legend(false).
				Viz(false)).
			ThresholdsStyle(common.NewGraphThresholdsStyleConfigBuilder().
				Mode("off")).
			AxisBorderShow(false)).
		WithRow(dashboard.NewRowBuilder("By Region").
			Title("By Region").
			GridPos(dashboard.GridPos{H: 1, W: 24, X: 0, Y: 87}).
			Id(0x4e)).
		WithPanel(timeseries.NewPanelBuilder().
			Id(0x1e).
			Targets([]cog.Builder[variants.Dataquery]{prometheus.NewDataqueryBuilder().
				Expr("topk(10, histogram_quantile(0.95, sum by(region) (rate(cloudcost_exporter_collector_duration_seconds{cluster=~\"$cluster\", collector=~\"$collector\"}[$__rate_interval]))))").
				Range().
				LegendFormat("{{region}}").
				RefId("A").
				QueryType("range").
				Datasource(promDS)}).
			Title("P95 Top 10 Slowest Regions").
			Description("P95 collection duration trend over time, broken down by region. Shows up to the 10 slowest regions at each point in time.").
			Datasource(promDS).
			GridPos(dashboard.GridPos{H: 8, W: 12, X: 0, Y: 87}).
			Height(0x8).
			Unit("s").
			Thresholds(dashboard.NewThresholdsConfigBuilder().
				Mode("absolute").
				Steps([]dashboard.Threshold{dashboard.Threshold{Value: cog.ToPtr[float64](0), Color: "green"},
					dashboard.Threshold{Value: cog.ToPtr[float64](80), Color: "red"}})).
			ColorScheme(dashboard.NewFieldColorBuilder().
				Mode("palette-classic")).
			Legend(common.NewVizLegendOptionsBuilder().
				DisplayMode("list").
				Placement("right").
				ShowLegend(true)).
			Tooltip(common.NewVizTooltipOptionsBuilder().
				Mode("multi").
				Sort("desc").
				HideZeros(false)).
			DrawStyle("line").
			GradientMode("none").
			ThresholdsStyle(common.NewGraphThresholdsStyleConfigBuilder().
				Mode("off")).
			LineWidth(2).
			LineInterpolation("smooth").
			FillOpacity(5).
			ShowPoints("never").
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
			AxisBorderShow(false)).
		WithPanel(timeseries.NewPanelBuilder().
			Id(0x1f).
			Targets([]cog.Builder[variants.Dataquery]{prometheus.NewDataqueryBuilder().
				Expr("histogram_quantile(0.95, sum by(region) (rate(cloudcost_exporter_collector_duration_seconds{cluster=~\"$cluster\", collector=~\"$collector\", region=~\".*[a-z]-[a-z]+[0-9]+$\"}[$__rate_interval])))").
				Range().
				LegendFormat("{{region}}").
				RefId("A").
				QueryType("range").
				Datasource(promDS)}).
			Title("P95 Duration — GCP Regions").
			Description("GCP regions identified by naming convention (no hyphen before trailing digit, e.g. europe-west4, us-central1).").
			Datasource(promDS).
			GridPos(dashboard.GridPos{H: 8, W: 12, X: 12, Y: 87}).
			Height(0x8).
			Unit("s").
			Thresholds(dashboard.NewThresholdsConfigBuilder().
				Mode("absolute").
				Steps([]dashboard.Threshold{dashboard.Threshold{Value: cog.ToPtr[float64](0), Color: "green"},
					dashboard.Threshold{Value: cog.ToPtr[float64](80), Color: "red"}})).
			ColorScheme(dashboard.NewFieldColorBuilder().
				Mode("palette-classic")).
			Legend(common.NewVizLegendOptionsBuilder().
				DisplayMode("list").
				Placement("right").
				ShowLegend(true)).
			Tooltip(common.NewVizTooltipOptionsBuilder().
				Mode("multi").
				Sort("desc").
				HideZeros(false)).
			DrawStyle("line").
			GradientMode("none").
			ThresholdsStyle(common.NewGraphThresholdsStyleConfigBuilder().
				Mode("off")).
			LineWidth(2).
			LineInterpolation("smooth").
			FillOpacity(5).
			ShowPoints("never").
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
			AxisBorderShow(false)).
		WithPanel(timeseries.NewPanelBuilder().
			Id(0x20).
			Targets([]cog.Builder[variants.Dataquery]{prometheus.NewDataqueryBuilder().
				Expr("histogram_quantile(0.95, sum by(region) (rate(cloudcost_exporter_collector_duration_seconds{cluster=~\"$cluster\", collector=~\"$collector\", region=~\".*-[0-9]+$\"}[$__rate_interval])))").
				Range().
				LegendFormat("{{region}}").
				RefId("A").
				QueryType("range").
				Datasource(promDS)}).
			Title("P95 Duration — AWS Regions").
			Description("AWS regions identified by naming convention (hyphen before trailing digit, e.g. us-east-1, eu-west-2).").
			Datasource(promDS).
			GridPos(dashboard.GridPos{H: 8, W: 12, X: 0, Y: 95}).
			Height(0x8).
			Unit("s").
			Thresholds(dashboard.NewThresholdsConfigBuilder().
				Mode("absolute").
				Steps([]dashboard.Threshold{dashboard.Threshold{Value: cog.ToPtr[float64](0), Color: "green"},
					dashboard.Threshold{Value: cog.ToPtr[float64](80), Color: "red"}})).
			ColorScheme(dashboard.NewFieldColorBuilder().
				Mode("palette-classic")).
			Legend(common.NewVizLegendOptionsBuilder().
				DisplayMode("list").
				Placement("right").
				ShowLegend(true)).
			Tooltip(common.NewVizTooltipOptionsBuilder().
				Mode("multi").
				Sort("desc").
				HideZeros(false)).
			DrawStyle("line").
			GradientMode("none").
			ThresholdsStyle(common.NewGraphThresholdsStyleConfigBuilder().
				Mode("off")).
			LineWidth(2).
			LineInterpolation("smooth").
			FillOpacity(5).
			ShowPoints("never").
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
			AxisBorderShow(false)).
		WithPanel(timeseries.NewPanelBuilder().
			Id(0x21).
			Targets([]cog.Builder[variants.Dataquery]{prometheus.NewDataqueryBuilder().
				Expr("histogram_quantile(0.95, sum by(region) (rate(cloudcost_exporter_collector_duration_seconds{cluster=~\"$cluster\", collector=~\"$collector\", region=~\"[a-z][a-z0-9]+\"}[$__rate_interval])))").
				Range().
				LegendFormat("{{region}}").
				RefId("A").
				QueryType("range").
				Datasource(promDS)}).
			Title("P95 Duration — Azure Regions").
			Description("Azure regions identified by naming convention (no hyphens, e.g. eastus, westeurope, eastus2).").
			Datasource(promDS).
			GridPos(dashboard.GridPos{H: 8, W: 12, X: 12, Y: 95}).
			Height(0x8).
			Unit("s").
			Thresholds(dashboard.NewThresholdsConfigBuilder().
				Mode("absolute").
				Steps([]dashboard.Threshold{dashboard.Threshold{Value: cog.ToPtr[float64](0), Color: "green"},
					dashboard.Threshold{Value: cog.ToPtr[float64](80), Color: "red"}})).
			ColorScheme(dashboard.NewFieldColorBuilder().
				Mode("palette-classic")).
			Legend(common.NewVizLegendOptionsBuilder().
				DisplayMode("list").
				Placement("right").
				ShowLegend(true)).
			Tooltip(common.NewVizTooltipOptionsBuilder().
				Mode("multi").
				Sort("desc").
				HideZeros(false)).
			DrawStyle("line").
			GradientMode("none").
			ThresholdsStyle(common.NewGraphThresholdsStyleConfigBuilder().
				Mode("off")).
			LineWidth(2).
			LineInterpolation("smooth").
			FillOpacity(5).
			ShowPoints("never").
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
			AxisBorderShow(false))
	return builder
}
