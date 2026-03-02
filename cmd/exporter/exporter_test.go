package main

import (
	"testing"

	"github.com/grafana/cloudcost-exporter/cmd/exporter/config"
)

func Test_regionFromConfig(t *testing.T) {
	tests := map[string]struct {
		provider  string
		awsRegion string
		gcpRegion string
		want      string
	}{
		"aws returns aws region": {
			provider:  "aws",
			awsRegion: "me-central-1",
			want:      "me-central-1",
		},
		"gcp returns gcp region": {
			provider:  "gcp",
			gcpRegion: "us-central1",
			want:      "us-central1",
		},
		"azure returns empty string": {
			provider: "azure",
			want:     "",
		},
		"unknown provider returns empty string": {
			provider: "unknown",
			want:     "",
		},
		"aws with empty region returns empty string": {
			provider:  "aws",
			awsRegion: "",
			want:      "",
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			cfg := &config.Config{Provider: tc.provider}
			cfg.Providers.AWS.Region = tc.awsRegion
			cfg.Providers.GCP.Region = tc.gcpRegion

			got := regionFromConfig(cfg)
			if got != tc.want {
				t.Errorf("regionFromConfig() = %q, want %q", got, tc.want)
			}
		})
	}
}
