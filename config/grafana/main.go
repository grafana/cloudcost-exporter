package main

import (
	"fmt"
	"log"

	"github.com/grafana/cloudcost-exporter/config/grafana/dashboards"
)

func main() {
	dashes, err := dashboards.BuildDashboards()
	if err != nil {
		log.Fatalf("failed to build dashboard: %v", err)
	}
	for _, content := range dashes {
		fmt.Println(content)
	}
	// TODO: check if the content should be written out to files
}
