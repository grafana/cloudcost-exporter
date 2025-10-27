package client

import (
	"fmt"

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

func (s *SQLAdmin) listInstances(projectId string) ([]*sqladmin.DatabaseInstance, error) {
	instances, err := s.sqlAdminService.Instances.List(projectId).Do()
	if err != nil {
		return nil, err
	}

	for _, instance := range instances.Items {
		fmt.Println(instance)
	}

	return instances.Items, nil
}
