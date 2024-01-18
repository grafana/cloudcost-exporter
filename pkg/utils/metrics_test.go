package utils

import (
	"testing"
)

func Test_parseFqNameFromMetric(t *testing.T) {
	tests := map[string]struct {
		arg  string
		want string
	}{
		"empty metric": {
			arg:  "",
			want: "",
		},
		"metric with fqName": {
			arg:  "fqName: \"aws_s3_bucket_size_bytes\"",
			want: "aws_s3_bucket_size_bytes",
		},
		"metric with fqName and help": {
			arg:  "FqName:\"Desc{fqName: \"cloudcost_exporter_gcp_collector_success\", help: \"Was the last scrape of the GCP metrics successful.\", constLabels: {}, variableLabels: {collector}}\"",
			want: "cloudcost_exporter_gcp_collector_success",
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			if got := parseFqNameFromMetric(tt.arg); got != tt.want {
				t.Errorf("parseFqNameFromMetric() = %v, want %v", got, tt.want)
			}
		})
	}
}
