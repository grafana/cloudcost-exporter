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
	"github.com/grafana/grafana-foundation-sdk/go/timeseries"
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
		Version(0x9).
		Variables([]cog.Builder[dashboard.VariableModel]{dashboard.NewDatasourceVariableBuilder("datasource").
			Label("Data Source").
			Type("prometheus"),
			dashboard.NewDatasourceVariableBuilder("loki_datasource").
				Label("Loki Data Source").
				Type("loki"),
			dashboard.NewQueryVariableBuilder("cluster").
				Name("cluster").
				Label("Cluster").
				Hide(0).
				Query(dashboard.StringOrMap{Map: map[string]interface{}{"label": "cluster", "metric": "cloudcost_exporter_collector_duration_seconds", "qryType": 1, "query": "label_values(cloudcost_exporter_collector_duration_seconds,cluster)", "refId": "PrometheusVariableQueryEditor-VariableQuery"}}).
				Datasource(common.DataSourceRef{Type: cog.ToPtr[string]("prometheus"), Uid: cog.ToPtr[string]("${datasource}")}).
				Current(dashboard.VariableOption{Text: dashboard.StringOrArrayOfString{String: cog.ToPtr[string]("All")}, Value: dashboard.StringOrArrayOfString{ArrayOfString: []string{"$__all"}}}).
				Multi(true).
				Refresh(1).
				Sort(1).
				IncludeAll(true),
			dashboard.NewQueryVariableBuilder("collector").
				Name("collector").
				Label("Collector").
				Hide(0).
				Query(dashboard.StringOrMap{Map: map[string]interface{}{"label": "collector", "metric": "cloudcost_exporter_collector_duration_seconds", "qryType": 1, "query": "label_values(cloudcost_exporter_collector_duration_seconds,collector)", "refId": "PrometheusVariableQueryEditor-VariableQuery"}}).
				Datasource(common.DataSourceRef{Type: cog.ToPtr[string]("prometheus"), Uid: cog.ToPtr[string]("${datasource}")}).
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
				Datasource(common.DataSourceRef{Type: cog.ToPtr[string]("prometheus"), Uid: cog.ToPtr[string]("${datasource}")}),
				prometheus.NewDataqueryBuilder().
					Expr("count(count by(collector) (cloudcost_exporter_collector_duration_seconds{cluster=~\"$cluster\", collector=~\"$collector\"}))").
					Instant().
					LegendFormat("Total").
					RefId("B").
					QueryType("instant").
					Datasource(common.DataSourceRef{Type: cog.ToPtr[string]("prometheus"), Uid: cog.ToPtr[string]("${datasource}")})}).
			Title("Active Collectors").
			Description("Number of distinct collectors currently reporting duration metrics.").
			Datasource(common.DataSourceRef{Type: cog.ToPtr[string]("prometheus"), Uid: cog.ToPtr[string]("${datasource}")}).
			GridPos(dashboard.GridPos{H: 9, W: 8, X: 0, Y: 1}).
			Span(0x8).
			Unit("short").
			Thresholds(dashboard.NewThresholdsConfigBuilder().
				Mode("absolute").
				Steps([]dashboard.Threshold{{Value: cog.ToPtr[float64](0), Color: "green"},
					{Value: cog.ToPtr[float64](80), Color: "red"}})).
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
				Datasource(common.DataSourceRef{Type: cog.ToPtr[string]("prometheus"), Uid: cog.ToPtr[string]("${datasource}")})}).
			Title("P95 Collection Duration").
			Description("95th percentile of collector run duration across all selected clusters and collectors.").
			Datasource(common.DataSourceRef{Type: cog.ToPtr[string]("prometheus"), Uid: cog.ToPtr[string]("${datasource}")}).
			GridPos(dashboard.GridPos{H: 9, W: 8, X: 8, Y: 1}).
			Span(0x8).
			Unit("s").
			Thresholds(dashboard.NewThresholdsConfigBuilder().
				Mode("absolute").
				Steps([]dashboard.Threshold{{Value: cog.ToPtr[float64](0), Color: "green"},
					{Value: cog.ToPtr[float64](30), Color: "yellow"},
					{Value: cog.ToPtr[float64](120), Color: "red"}})).
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
				Expr("0 * sum(rate(cloudcost_exporter_collector_total{cluster=~\"$cluster\", collector=~\"$collector\"}[$__rate_interval])) + 100").
				Instant().
				LegendFormat("Success Rate").
				RefId("A").
				QueryType("instant").
				Datasource(common.DataSourceRef{Type: cog.ToPtr[string]("prometheus"), Uid: cog.ToPtr[string]("${datasource}")})}).
			Title("Success Rate").
			Description("Percentage of successful collector runs (total - errors) / total.").
			Datasource(common.DataSourceRef{Type: cog.ToPtr[string]("prometheus"), Uid: cog.ToPtr[string]("${datasource}")}).
			GridPos(dashboard.GridPos{H: 9, W: 8, X: 16, Y: 1}).
			Span(0x8).
			Unit("percent").
			Min(0).
			Max(100).
			Thresholds(dashboard.NewThresholdsConfigBuilder().
				Mode("absolute").
				Steps([]dashboard.Threshold{{Value: cog.ToPtr[float64](0), Color: "red"},
					{Value: cog.ToPtr[float64](90), Color: "yellow"},
					{Value: cog.ToPtr[float64](95), Color: "green"}})).
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
				Expr("0 * sum by(collector) (rate(cloudcost_exporter_collector_total{cluster=~\"$cluster\", collector=~\"$collector\"}[$__rate_interval]))").
				Range().
				LegendFormat("{{collector}}").
				RefId("A").
				QueryType("range").
				Datasource(common.DataSourceRef{Type: cog.ToPtr[string]("prometheus"), Uid: cog.ToPtr[string]("${datasource}")})}).
			Title("Error Rate by Collector (%)").
			Description("Per-collector error rate over time — errors / total collector runs.").
			Datasource(common.DataSourceRef{Type: cog.ToPtr[string]("prometheus"), Uid: cog.ToPtr[string]("${datasource}")}).
			GridPos(dashboard.GridPos{H: 9, W: 8, X: 0, Y: 10}).
			Span(0x8).
			Unit("percent").
			Min(0).
			Thresholds(dashboard.NewThresholdsConfigBuilder().
				Mode("absolute").
				Steps([]dashboard.Threshold{{Value: cog.ToPtr[float64](0), Color: "green"}})).
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
				Expr("0 * sum by(collector) (rate(cloudcost_exporter_collector_total{cluster=~\"$cluster\", collector=~\"$collector\"}[$__rate_interval])) + 100").
				Range().
				LegendFormat("{{collector}}").
				RefId("A").
				QueryType("range").
				Datasource(common.DataSourceRef{Type: cog.ToPtr[string]("prometheus"), Uid: cog.ToPtr[string]("${datasource}")})}).
			Title("Success Rate by Collector (%)").
			Description("Per-collector success rate over time.").
			Datasource(common.DataSourceRef{Type: cog.ToPtr[string]("prometheus"), Uid: cog.ToPtr[string]("${datasource}")}).
			GridPos(dashboard.GridPos{H: 9, W: 8, X: 8, Y: 10}).
			Span(0x8).
			Unit("percent").
			Min(0).
			Max(100).
			Thresholds(dashboard.NewThresholdsConfigBuilder().
				Mode("absolute").
				Steps([]dashboard.Threshold{{Value: cog.ToPtr[float64](0), Color: "green"}})).
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
				Datasource(common.DataSourceRef{Type: cog.ToPtr[string]("prometheus"), Uid: cog.ToPtr[string]("${datasource}")}),
				prometheus.NewDataqueryBuilder().
					Expr("0 * sum(rate(cloudcost_exporter_collector_total{cluster=~\"$cluster\", collector=~\"$collector\"}[$__rate_interval]))").
					Range().
					LegendFormat("Errors/s").
					RefId("B").
					QueryType("range").
					Datasource(common.DataSourceRef{Type: cog.ToPtr[string]("prometheus"), Uid: cog.ToPtr[string]("${datasource}")})}).
			Title("Collection Rate vs Error Rate").
			Description("Collection attempt rate vs error rate — shows the error share relative to total throughput.").
			Datasource(common.DataSourceRef{Type: cog.ToPtr[string]("prometheus"), Uid: cog.ToPtr[string]("${datasource}")}).
			GridPos(dashboard.GridPos{H: 9, W: 8, X: 16, Y: 10}).
			Span(0x8).
			Unit("ops").
			Thresholds(dashboard.NewThresholdsConfigBuilder().
				Mode("absolute").
				Steps([]dashboard.Threshold{{Value: cog.ToPtr[float64](0), Color: "green"}})).
			ColorScheme(dashboard.NewFieldColorBuilder().
				Mode("palette-classic")).
			Overrides([]cog.Builder[dashboard.DashboardFieldConfigSourceOverrides]{dashboard.NewDashboardFieldConfigSourceOverridesBuilder().
				Matcher(dashboard.MatcherConfig{Id: "byName", Options: "Errors/s"}).
				Properties([]dashboard.DynamicConfigValue{{Id: "color", Value: map[string]interface{}{"fixedColor": "red", "mode": "fixed"}}})}).
			WithOverride(dashboard.MatcherConfig{Id: "byName", Options: "Errors/s"}, []dashboard.DynamicConfigValue{{Id: "color", Value: map[string]interface{}{"fixedColor": "red", "mode": "fixed"}}}).
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
		WithPanel(timeseries.NewPanelBuilder().
			Id(0x46).
			Targets([]cog.Builder[variants.Dataquery]{prometheus.NewDataqueryBuilder().
				Expr("(max by(cluster) (cloudcost_exporter_build_info{version=\"v0.29.1\"}) * 3)\nor\n(max by(cluster) (cloudcost_exporter_build_info{version=\"v0.28.1\"}) * 2)\nor\n(max by(cluster) (cloudcost_exporter_build_info{version=\"v0.25.0\"}) * 1)").
				Range().
				LegendFormat("{{cluster}}").
				RefId("A").
				QueryType("range").
				Datasource(common.DataSourceRef{Type: cog.ToPtr[string]("prometheus"), Uid: cog.ToPtr[string]("${datasource}")})}).
			Title("Version History per Environment").
			Description("Version deployed per cluster over time. Each coloured bar segment shows a running version; gaps indicate no data.").
			Datasource(common.DataSourceRef{Type: cog.ToPtr[string]("prometheus"), Uid: cog.ToPtr[string]("${datasource}")}).
			GridPos(dashboard.GridPos{H: 9, W: 24, X: 0, Y: 19}).
			Span(0x18).
			Decimals(0).
			Min(1).
			Max(3).
			Mappings([]dashboard.ValueMapping{dashboard.ValueMapOrRangeMapOrRegexMapOrSpecialValueMap{ValueMap: cog.ToPtr[dashboard.ValueMap](dashboard.ValueMap{Type: "value", Options: map[string]dashboard.ValueMappingResult{"1": {Text: cog.ToPtr[string]("v0.25.0")}, "2": {Text: cog.ToPtr[string]("v0.28.1")}, "3": {Text: cog.ToPtr[string]("v0.29.1")}}})}}).
			Thresholds(dashboard.NewThresholdsConfigBuilder().
				Mode("absolute").
				Steps([]dashboard.Threshold{{Value: cog.ToPtr[float64](0), Color: "green"},
					{Value: cog.ToPtr[float64](80), Color: "red"}})).
			ColorScheme(dashboard.NewFieldColorBuilder().
				Mode("palette-classic")).
			Legend(common.NewVizLegendOptionsBuilder().
				DisplayMode("list").
				Placement("bottom").
				ShowLegend(true)).
			Tooltip(common.NewVizTooltipOptionsBuilder().
				Mode("single").
				Sort("none").
				HideZeros(false)).
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
			AxisSoftMin(0.9).
			AxisSoftMax(3.1).
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
		WithRow(dashboard.NewRowBuilder("By Provider").
			Title("By Provider").
			GridPos(dashboard.GridPos{H: 1, W: 24, X: 0, Y: 28}).
			Id(0x49)).
		WithPanel(stat.NewPanelBuilder().
			Id(0x40).
			Targets([]cog.Builder[variants.Dataquery]{prometheus.NewDataqueryBuilder().
				Expr("histogram_quantile(0.95, sum by(le) (rate(cloudcost_exporter_collector_duration_seconds{cluster=~\"$cluster\", collector=~\"aws_ec2|aws_elb|aws_rds|S3|NATGATEWAY|VPC\"}[$__rate_interval])))").
				Instant().
				LegendFormat("AWS").
				RefId("A").
				QueryType("instant").
				Datasource(common.DataSourceRef{Type: cog.ToPtr[string]("prometheus"), Uid: cog.ToPtr[string]("${datasource}")})}).
			Title("P95 Duration — AWS").
			Description("P95 collection duration across AWS collectors.").
			Datasource(common.DataSourceRef{Type: cog.ToPtr[string]("prometheus"), Uid: cog.ToPtr[string]("${datasource}")}).
			GridPos(dashboard.GridPos{H: 6, W: 8, X: 0, Y: 28}).
			Height(0x6).
			Span(0x8).
			Unit("s").
			Thresholds(dashboard.NewThresholdsConfigBuilder().
				Mode("absolute").
				Steps([]dashboard.Threshold{{Value: cog.ToPtr[float64](0), Color: "green"},
					{Value: cog.ToPtr[float64](30), Color: "yellow"},
					{Value: cog.ToPtr[float64](120), Color: "red"}})).
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
				Expr("histogram_quantile(0.95, sum by(le) (rate(cloudcost_exporter_collector_duration_seconds{cluster=~\"$cluster\", collector=~\"gcp_gke|GCS|ForwardingRule\"}[$__rate_interval])))").
				Instant().
				LegendFormat("GCP").
				RefId("A").
				QueryType("instant").
				Datasource(common.DataSourceRef{Type: cog.ToPtr[string]("prometheus"), Uid: cog.ToPtr[string]("${datasource}")})}).
			Title("P95 Duration — GCP").
			Description("P95 collection duration across GCP collectors.").
			Datasource(common.DataSourceRef{Type: cog.ToPtr[string]("prometheus"), Uid: cog.ToPtr[string]("${datasource}")}).
			GridPos(dashboard.GridPos{H: 6, W: 8, X: 8, Y: 28}).
			Height(0x6).
			Span(0x8).
			Unit("s").
			Thresholds(dashboard.NewThresholdsConfigBuilder().
				Mode("absolute").
				Steps([]dashboard.Threshold{{Value: cog.ToPtr[float64](0), Color: "green"},
					{Value: cog.ToPtr[float64](30), Color: "yellow"},
					{Value: cog.ToPtr[float64](120), Color: "red"}})).
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
				Expr("histogram_quantile(0.95, sum by(le) (rate(cloudcost_exporter_collector_duration_seconds{cluster=~\"$cluster\", collector=~\"azure_aks\"}[$__rate_interval])))").
				Instant().
				LegendFormat("Azure").
				RefId("A").
				QueryType("instant").
				Datasource(common.DataSourceRef{Type: cog.ToPtr[string]("prometheus"), Uid: cog.ToPtr[string]("${datasource}")})}).
			Title("P95 Duration — Azure").
			Description("P95 collection duration across Azure collectors.").
			Datasource(common.DataSourceRef{Type: cog.ToPtr[string]("prometheus"), Uid: cog.ToPtr[string]("${datasource}")}).
			GridPos(dashboard.GridPos{H: 6, W: 8, X: 16, Y: 28}).
			Height(0x6).
			Span(0x8).
			Unit("s").
			Thresholds(dashboard.NewThresholdsConfigBuilder().
				Mode("absolute").
				Steps([]dashboard.Threshold{{Value: cog.ToPtr[float64](0), Color: "green"},
					{Value: cog.ToPtr[float64](30), Color: "yellow"},
					{Value: cog.ToPtr[float64](120), Color: "red"}})).
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
				Expr("0 * sum(rate(cloudcost_exporter_collector_total{cluster=~\"$cluster\", collector=~\"aws_ec2|aws_elb|aws_rds|S3|NATGATEWAY|VPC\"}[$__rate_interval])) + 100").
				Instant().
				LegendFormat("AWS").
				RefId("A").
				QueryType("instant").
				Datasource(common.DataSourceRef{Type: cog.ToPtr[string]("prometheus"), Uid: cog.ToPtr[string]("${datasource}")})}).
			Title("Success Rate — AWS").
			Description("Success rate for AWS collectors (errors assumed 0).").
			Datasource(common.DataSourceRef{Type: cog.ToPtr[string]("prometheus"), Uid: cog.ToPtr[string]("${datasource}")}).
			GridPos(dashboard.GridPos{H: 6, W: 8, X: 0, Y: 34}).
			Height(0x6).
			Span(0x8).
			Unit("percent").
			Min(0).
			Max(100).
			Thresholds(dashboard.NewThresholdsConfigBuilder().
				Mode("absolute").
				Steps([]dashboard.Threshold{{Value: cog.ToPtr[float64](0), Color: "red"},
					{Value: cog.ToPtr[float64](90), Color: "yellow"},
					{Value: cog.ToPtr[float64](95), Color: "green"}})).
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
				Expr("0 * sum(rate(cloudcost_exporter_collector_total{cluster=~\"$cluster\", collector=~\"gcp_gke|GCS|ForwardingRule\"}[$__rate_interval])) + 100").
				Instant().
				LegendFormat("GCP").
				RefId("A").
				QueryType("instant").
				Datasource(common.DataSourceRef{Type: cog.ToPtr[string]("prometheus"), Uid: cog.ToPtr[string]("${datasource}")})}).
			Title("Success Rate — GCP").
			Description("Success rate for GCP collectors (errors assumed 0).").
			Datasource(common.DataSourceRef{Type: cog.ToPtr[string]("prometheus"), Uid: cog.ToPtr[string]("${datasource}")}).
			GridPos(dashboard.GridPos{H: 6, W: 8, X: 8, Y: 34}).
			Height(0x6).
			Span(0x8).
			Unit("percent").
			Min(0).
			Max(100).
			Thresholds(dashboard.NewThresholdsConfigBuilder().
				Mode("absolute").
				Steps([]dashboard.Threshold{{Value: cog.ToPtr[float64](0), Color: "red"},
					{Value: cog.ToPtr[float64](90), Color: "yellow"},
					{Value: cog.ToPtr[float64](95), Color: "green"}})).
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
				Expr("0 * sum(rate(cloudcost_exporter_collector_total{cluster=~\"$cluster\", collector=~\"azure_aks\"}[$__rate_interval])) + 100").
				Instant().
				LegendFormat("Azure").
				RefId("A").
				QueryType("instant").
				Datasource(common.DataSourceRef{Type: cog.ToPtr[string]("prometheus"), Uid: cog.ToPtr[string]("${datasource}")})}).
			Title("Success Rate — Azure").
			Description("Success rate for Azure collectors (errors assumed 0).").
			Datasource(common.DataSourceRef{Type: cog.ToPtr[string]("prometheus"), Uid: cog.ToPtr[string]("${datasource}")}).
			GridPos(dashboard.GridPos{H: 6, W: 8, X: 16, Y: 34}).
			Height(0x6).
			Span(0x8).
			Unit("percent").
			Min(0).
			Max(100).
			Thresholds(dashboard.NewThresholdsConfigBuilder().
				Mode("absolute").
				Steps([]dashboard.Threshold{{Value: cog.ToPtr[float64](0), Color: "red"},
					{Value: cog.ToPtr[float64](90), Color: "yellow"},
					{Value: cog.ToPtr[float64](95), Color: "green"}})).
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
				Datasource(common.DataSourceRef{Type: cog.ToPtr[string]("loki"), Uid: cog.ToPtr[string]("${loki_datasource}")})}).
			Title("Error Log Rate").
			Description("Rate of log lines containing error-level messages from the cloudcost-exporter namespace.").
			Datasource(common.DataSourceRef{Type: cog.ToPtr[string]("loki"), Uid: cog.ToPtr[string]("${loki_datasource}")}).
			GridPos(dashboard.GridPos{H: 8, W: 24, X: 0, Y: 40}).
			Height(0x8).
			Span(0x18).
			Unit("short").
			Thresholds(dashboard.NewThresholdsConfigBuilder().
				Mode("absolute").
				Steps([]dashboard.Threshold{{Value: cog.ToPtr[float64](0), Color: "green"}})).
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
				Datasource(common.DataSourceRef{Type: cog.ToPtr[string]("loki"), Uid: cog.ToPtr[string]("${loki_datasource}")})}).
			Title("Error Logs").
			Description("Log lines at error level from the cloudcost-exporter namespace.").
			Datasource(common.DataSourceRef{Type: cog.ToPtr[string]("loki"), Uid: cog.ToPtr[string]("${loki_datasource}")}).
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
				Datasource(common.DataSourceRef{Type: cog.ToPtr[string]("loki"), Uid: cog.ToPtr[string]("${loki_datasource}")})}).
			Title("All Logs (cloudcost-exporter namespace)").
			Description("All logs from the cloudcost-exporter namespace across selected clusters.").
			Datasource(common.DataSourceRef{Type: cog.ToPtr[string]("loki"), Uid: cog.ToPtr[string]("${loki_datasource}")}).
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
				Datasource(common.DataSourceRef{Type: cog.ToPtr[string]("prometheus"), Uid: cog.ToPtr[string]("${datasource}")})}).
			Title("P99 Duration by Collector").
			Description("P50, P95, and P99 latency percentiles of collection duration per collector over time.").
			Datasource(common.DataSourceRef{Type: cog.ToPtr[string]("prometheus"), Uid: cog.ToPtr[string]("${datasource}")}).
			GridPos(dashboard.GridPos{H: 9, W: 8, X: 0, Y: 69}).
			Span(0x8).
			Unit("s").
			Thresholds(dashboard.NewThresholdsConfigBuilder().
				Mode("absolute").
				Steps([]dashboard.Threshold{{Value: cog.ToPtr[float64](0), Color: "green"},
					{Value: cog.ToPtr[float64](80), Color: "red"}})).
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
				Datasource(common.DataSourceRef{Type: cog.ToPtr[string]("prometheus"), Uid: cog.ToPtr[string]("${datasource}")})}).
			Title("P95 Duration by Collector").
			Description("P95 collection duration trend per collector.").
			Datasource(common.DataSourceRef{Type: cog.ToPtr[string]("prometheus"), Uid: cog.ToPtr[string]("${datasource}")}).
			GridPos(dashboard.GridPos{H: 9, W: 8, X: 8, Y: 69}).
			Span(0x8).
			Unit("s").
			Thresholds(dashboard.NewThresholdsConfigBuilder().
				Mode("absolute").
				Steps([]dashboard.Threshold{{Value: cog.ToPtr[float64](0), Color: "green"},
					{Value: cog.ToPtr[float64](80), Color: "red"}})).
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
				Datasource(common.DataSourceRef{Type: cog.ToPtr[string]("prometheus"), Uid: cog.ToPtr[string]("${datasource}")})}).
			Title("P50 Duration by Collector").
			Description("P50 (median) collection duration trend per collector.").
			Datasource(common.DataSourceRef{Type: cog.ToPtr[string]("prometheus"), Uid: cog.ToPtr[string]("${datasource}")}).
			GridPos(dashboard.GridPos{H: 9, W: 8, X: 16, Y: 69}).
			Span(0x8).
			Unit("s").
			Thresholds(dashboard.NewThresholdsConfigBuilder().
				Mode("absolute").
				Steps([]dashboard.Threshold{{Value: cog.ToPtr[float64](0), Color: "green"},
					{Value: cog.ToPtr[float64](80), Color: "red"}})).
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
				Datasource(common.DataSourceRef{Type: cog.ToPtr[string]("prometheus"), Uid: cog.ToPtr[string]("${datasource}")})}).
			Title("P95 Duration by Collector (current, ranked)").
			Description("Current P95 collection duration ranked by collector. Identifies the slowest collectors at a glance.").
			Datasource(common.DataSourceRef{Type: cog.ToPtr[string]("prometheus"), Uid: cog.ToPtr[string]("${datasource}")}).
			GridPos(dashboard.GridPos{H: 8, W: 24, X: 0, Y: 78}).
			Height(0x8).
			Span(0x18).
			Unit("s").
			Thresholds(dashboard.NewThresholdsConfigBuilder().
				Mode("absolute").
				Steps([]dashboard.Threshold{{Value: cog.ToPtr[float64](0), Color: "green"},
					{Value: cog.ToPtr[float64](80), Color: "red"}})).
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
				Datasource(common.DataSourceRef{Type: cog.ToPtr[string]("prometheus"), Uid: cog.ToPtr[string]("${datasource}")})}).
			Title("P95 Top 10 Slowest Regions").
			Description("P95 collection duration trend over time, broken down by region.").
			Datasource(common.DataSourceRef{Type: cog.ToPtr[string]("prometheus"), Uid: cog.ToPtr[string]("${datasource}")}).
			GridPos(dashboard.GridPos{H: 8, W: 12, X: 0, Y: 87}).
			Height(0x8).
			Unit("s").
			Thresholds(dashboard.NewThresholdsConfigBuilder().
				Mode("absolute").
				Steps([]dashboard.Threshold{{Value: cog.ToPtr[float64](0), Color: "green"},
					{Value: cog.ToPtr[float64](80), Color: "red"}})).
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
				Expr("histogram_quantile(0.95, sum by(region) (rate(cloudcost_exporter_collector_duration_seconds{cluster=~\"$cluster\", collector=~\"$collector\", region=~\".*[a-z][0-9]+$\"}[$__rate_interval])))").
				Range().
				LegendFormat("{{region}}").
				RefId("A").
				QueryType("range").
				Datasource(common.DataSourceRef{Type: cog.ToPtr[string]("prometheus"), Uid: cog.ToPtr[string]("${datasource}")})}).
			Title("P95 Duration — GCP Regions").
			Description("GCP regions identified by naming convention (no hyphen before trailing digit, e.g. europe-west4, us-central1).").
			Datasource(common.DataSourceRef{Type: cog.ToPtr[string]("prometheus"), Uid: cog.ToPtr[string]("${datasource}")}).
			GridPos(dashboard.GridPos{H: 8, W: 12, X: 12, Y: 87}).
			Height(0x8).
			Unit("s").
			Thresholds(dashboard.NewThresholdsConfigBuilder().
				Mode("absolute").
				Steps([]dashboard.Threshold{{Value: cog.ToPtr[float64](0), Color: "green"},
					{Value: cog.ToPtr[float64](80), Color: "red"}})).
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
				Datasource(common.DataSourceRef{Type: cog.ToPtr[string]("prometheus"), Uid: cog.ToPtr[string]("${datasource}")})}).
			Title("P95 Duration — AWS Regions").
			Description("AWS regions identified by naming convention (hyphen before trailing digit, e.g. us-east-1, eu-west-2).").
			Datasource(common.DataSourceRef{Type: cog.ToPtr[string]("prometheus"), Uid: cog.ToPtr[string]("${datasource}")}).
			GridPos(dashboard.GridPos{H: 8, W: 12, X: 0, Y: 95}).
			Height(0x8).
			Unit("s").
			Thresholds(dashboard.NewThresholdsConfigBuilder().
				Mode("absolute").
				Steps([]dashboard.Threshold{{Value: cog.ToPtr[float64](0), Color: "green"},
					{Value: cog.ToPtr[float64](80), Color: "red"}})).
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
				Datasource(common.DataSourceRef{Type: cog.ToPtr[string]("prometheus"), Uid: cog.ToPtr[string]("${datasource}")})}).
			Title("P95 Duration — Azure Regions").
			Description("Azure regions identified by naming convention (no hyphens, e.g. eastus, westeurope, eastus2).").
			Datasource(common.DataSourceRef{Type: cog.ToPtr[string]("prometheus"), Uid: cog.ToPtr[string]("${datasource}")}).
			GridPos(dashboard.GridPos{H: 8, W: 12, X: 12, Y: 95}).
			Height(0x8).
			Unit("s").
			Thresholds(dashboard.NewThresholdsConfigBuilder().
				Mode("absolute").
				Steps([]dashboard.Threshold{{Value: cog.ToPtr[float64](0), Color: "green"},
					{Value: cog.ToPtr[float64](80), Color: "red"}})).
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
