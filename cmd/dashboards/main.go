package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/grafana/grafana-foundation-sdk/go/dashboard"

	"github.com/grafana/cloudcost-exporter/cloudcost-exporter-dashboards/dashboards"
)

func main() {
	output := flag.String("output", "console", "Where to write output to. Can be console or file")
	outputDir := flag.String("output-dir", "./cloudcost-exporter-dashboards/grafana", "output directory")
	flag.Parse()
	dashes := dashboards.BuildDashboards()

	err := run(dashes, output, outputDir)
	if err != nil {
		log.Fatalf("error generating dashboards: %s", err.Error())
	}
}

func run(dashes []*dashboard.DashboardBuilder, output *string, outputDir *string) error {
	for _, dash := range dashes {
		build, err := dash.Build()
		if err != nil {
			return err
		}
		data, err := json.MarshalIndent(build, "", "  ")
		if err != nil {
			return err
		}
		if *output == "console" {
			fmt.Println(string(data))
			continue
		}

		err = os.WriteFile(fmt.Sprintf("%s/%s.json", *outputDir, sluggify(*build.Title)), data, 0644)
		if err != nil {
			return err
		}
	}
	return nil
}

// sluggify will take a string and convert the string
func sluggify(s string) string {
	s = strings.TrimSpace(s)
	return strings.ReplaceAll(strings.ToLower(s), " ", "-")
}
