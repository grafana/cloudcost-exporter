package client

import (
	"context"

	sqladmin "google.golang.org/api/sqladmin/v1beta4"
)

type SQLAdmin struct {
	sqlAdminService *sqladmin.Service
	projectId       string
}

func newSQLAdmin(sqlAdminService *sqladmin.Service, projectId string) *SQLAdmin {
	return &SQLAdmin{
		sqlAdminService: sqlAdminService,
		projectId:       projectId,
	}
}

func (s *SQLAdmin) listInstances(ctx context.Context, projectId string) ([]*sqladmin.DatabaseInstance, error) {
	instances, err := s.sqlAdminService.Instances.List(projectId).Context(ctx).Do()
	if err != nil {
		return nil, err
	}

	return instances.Items, nil
}
