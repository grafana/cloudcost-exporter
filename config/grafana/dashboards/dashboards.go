package dashboards

func BuildDashboards() (map[string]string, error) {
	operations, err := buildOperationsDashboard()
	if err != nil {
		return nil, err
	}
	return map[string]string{
		"operations": string(operations),
	}, nil
}
