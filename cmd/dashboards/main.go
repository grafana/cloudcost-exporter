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
	mode := flag.String("mode", "json", "output mode. Can be json or file")
	outputDir := flag.String("output-dir", "./cloudcost-exporter-dashboards/grafana", "output directory")
	flag.Parse()
	dashes := dashboards.BuildDashboards()

	err := run(dashes, mode, outputDir)
	if err != nil {
		log.Fatalf("error generating dashboards: %s", err.Error())
	}
}

func run(dashes []*dashboard.DashboardBuilder, mode *string, outputDir *string) error {
	for _, dash := range dashes {
		build, err := dash.Build()
		if err != nil {
			return err
		}
		output, err := json.MarshalIndent(build, "", "  ")
		if err != nil {
			return err
		}
		if *mode == "json" {
			fmt.Println(string(output))
			continue
		}
		err = createFile(*outputDir, sluggify(*build.Title), output)
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

func createFile(outputDir, name string, data []byte) error {
	file, err := os.Create(fmt.Sprintf("%s/%s.json", outputDir, name))
	if err != nil {
		return err
	}
	defer file.Close()
	_, err = file.Write(data)
	return err
}
