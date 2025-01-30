package dashboards

import (
	"github.com/grafana/grafana-foundation-sdk/go/dashboard"
)

func BuildDashboards() []*dashboard.DashboardBuilder {
	operationDashboard := OperationsDashboard()
	return []*dashboard.DashboardBuilder{
		operationDashboard,
	}
}
