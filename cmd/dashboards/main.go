package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/grafana/grafana-foundation-sdk/go/dashboard"
	"github.com/grafana/grafana-foundation-sdk/go/resource"

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

		if *output == "console" {
			data, err := json.MarshalIndent(build, "", "  ")
			if err != nil {
				return err
			}
			fmt.Println(string(data))
			continue
		}

		// Write Grafana App Platform manifest (required by grafanactl resources serve)
		manifest, err := resource.NewManifestBuilder().
			ApiVersion("dashboard.grafana.app/v1beta1").
			Kind("Dashboard").
			Metadata(resource.NewMetadataBuilder().Name(*build.Title)).
			Spec(build).
			Build()
		if err != nil {
			return fmt.Errorf("build manifest for %s: %w", *build.Title, err)
		}
		data, err := json.MarshalIndent(manifest, "", "  ")
		if err != nil {
			return err
		}
		slug := sluggify(*build.Title)
		path := filepath.Join(*outputDir, slug+".json")
		if err := os.WriteFile(path, data, 0644); err != nil {
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
