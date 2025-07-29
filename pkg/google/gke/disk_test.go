package gke

import (
	"testing"

	"github.com/grafana/cloudcost-exporter/pkg/google/client"
	"github.com/stretchr/testify/require"
	computev1 "google.golang.org/api/compute/v1"
)

func Test_extractLabelsFromDesc(t *testing.T) {
	tests := map[string]struct {
		description    string
		labels         map[string]string
		expectedLabels map[string]string
		wantErr        bool
	}{
		"Empty description should return an empty map": {
			description: "",
			// Label needs to be initialized to an empty map, otherwise the underlying method to write data to it will fail
			labels:         map[string]string{},
			expectedLabels: map[string]string{},
			wantErr:        false,
		},
		"Description not formatted as json should return an error": {
			description: "test",
			// Label needs to be initialized to an empty map, otherwise the underlying method to write data to it will fail
			labels:         map[string]string{},
			expectedLabels: map[string]string{},
			wantErr:        true,
		},
		"Description formatted as json should return a map": {
			description: `{"test": "test"}`,
			// Label needs to be initialized to an empty map, otherwise the underlying method to write data to it will fail
			labels:         map[string]string{},
			expectedLabels: map[string]string{"test": "test"},
			wantErr:        false,
		},
		"Description formatted as json with multiple keys should return a map": {
			description: `{"kubernetes.io/created-for/pv/name":"pvc-32613356-4cee-481d-902f-daa7223d14ab","kubernetes.io/created-for/pvc/name":"prometheus-server-data-prometheus-0","kubernetes.io/created-for/pvc/namespace":"prometheus"}`,
			// Label needs to be initialized to an empty map, otherwise the underlying method to write data to it will fail
			labels: map[string]string{},
			expectedLabels: map[string]string{
				"kubernetes.io/created-for/pv/name":       "pvc-32613356-4cee-481d-902f-daa7223d14ab",
				"kubernetes.io/created-for/pvc/name":      "prometheus-server-data-prometheus-0",
				"kubernetes.io/created-for/pvc/namespace": "prometheus",
			},
			wantErr: false,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			if err := extractLabelsFromDesc(tt.description, tt.labels); (err != nil) != tt.wantErr {
				t.Errorf("extractLabelsFromDesc() error = %v, wantErr %v", err, tt.wantErr)
			}
			require.Equal(t, tt.expectedLabels, tt.labels)
		})
	}
}

func Test_getNamespaceFromDisk(t *testing.T) {
	tests := map[string]struct {
		disk *Disk
		want string
	}{
		"Empty description should return an empty string": {
			disk: NewDisk(&computev1.Disk{
				Description: "",
			}, ""),
			want: "",
		},
		"Description not formatted as json should return an empty string": {
			disk: NewDisk(&computev1.Disk{
				Description: "test",
			}, ""),
			want: "",
		},
		"Description formatted as json with multiple keys should return a namespace": {
			disk: NewDisk(&computev1.Disk{
				Description: `{"kubernetes.io/created-for/pv/name":"pvc-32613356-4cee-481d-902f-daa7223d14ab","kubernetes.io/created-for/pvc/name":"prometheus","kubernetes.io/created-for/pvc/namespace":"prometheus"}`,
			}, ""),
			want: "prometheus",
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			if got := tt.disk.Namespace(); got != tt.want {
				t.Errorf("getNamespaceFromDisk() = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_getRegionFromDisk(t *testing.T) {
	tests := map[string]struct {
		disk *Disk
		want string
	}{
		"Empty zone should return an empty string": {
			disk: NewDisk(&computev1.Disk{
				Zone: "",
			}, ""),
			want: "",
		},
		"Zone formatted as a path should return the region": {
			disk: NewDisk(&computev1.Disk{
				Zone: "projects/123/zones/us-central1-a",
			}, ""),
			want: "us-central1",
		},
		"Disk with zone as label should return the region parsed properly": {
			disk: NewDisk(&computev1.Disk{
				Labels: map[string]string{
					client.GkeRegionLabel: "us-central1-f",
				},
			}, ""),
			want: "us-central1",
		},
		"Disk with a label doesn't belong to a specific zone should return the full label": {
			disk: NewDisk(&computev1.Disk{
				Labels: map[string]string{
					client.GkeRegionLabel: "us-central1",
				},
			}, ""),
			want: "us-central1",
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			if got := tt.disk.Region(); got != tt.want {
				t.Errorf("getRegionFromDisk() = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_getNameFromDisk(t *testing.T) {
	tests := map[string]struct {
		disk *Disk
		want string
	}{
		"Empty description should return an empty string": {
			disk: NewDisk(&computev1.Disk{
				Description: "",
			}, ""),
			want: "",
		},
		"Description not formatted as json should return an empty string": {
			disk: NewDisk(&computev1.Disk{
				Description: "test",
			}, ""),
			want: "",
		},
		"Description not formatted as json should return the disks name": {
			disk: NewDisk(&computev1.Disk{
				Description: "test",
				Name:        "testing123",
			}, ""),
			want: "testing123",
		},
		"Description formatted as json with multiple keys should return the name": {
			disk: NewDisk(&computev1.Disk{
				Description: `{"kubernetes.io/created-for/pv/name":"pvc-32613356-4cee-481d-902f-daa7223d14ab","kubernetes.io/created-for/pvc/name":"prometheus","kubernetes.io/created-for/pvc/namespace":"prometheus"}`,
			}, ""),
			want: "pvc-32613356-4cee-481d-902f-daa7223d14ab",
		},
		"Description formatted as json with one key should return the name": {
			disk: NewDisk(&computev1.Disk{
				Description: `{"kubernetes.io-created-for/pv-name":"pvc-32613356-4cee-481d-902f-daa7223d14ab"}`,
			}, ""),
			want: "pvc-32613356-4cee-481d-902f-daa7223d14ab",
		},
		"Description formatted as json with multiple wrong keys should return empty string": {
			disk: NewDisk(&computev1.Disk{
				Description: `{"kubernetes.io/created-for/pvc/name":"prometheus","kubernetes.io/created-for/pvc/namespace":"prometheus"}`,
			}, ""),
			want: "",
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			if got := tt.disk.Name(); got != tt.want {
				t.Errorf("getNameFromDisk() = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_getStorageClassFromDisk(t *testing.T) {
	tests := map[string]struct {
		disk *Disk
		want string
	}{
		"Empty Type should return an empty string": {
			disk: NewDisk(&computev1.Disk{
				Type: "",
			}, ""),
			want: "",
		},
		"Type formatted as a path should return the storage class": {
			disk: NewDisk(&computev1.Disk{
				Type: "projects/123/zones/us-central1-a/diskTypes/pd-standard",
			}, ""),
			want: "pd-standard",
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			if got := test.disk.StorageClass(); got != test.want {
				t.Errorf("getStorageClassFromDisk() = %v, want %v", got, test.want)
			}
		})
	}
}

func Test_DiskType(t *testing.T) {
	tests := map[string]struct {
		disk *Disk
		want string
	}{
		"Disk with no disk type returns default value": {
			disk: NewDisk(&computev1.Disk{}, ""),
			want: "persistent_volume",
		},
		"Disk with a boot disk label returns boot_disk": {
			disk: NewDisk(&computev1.Disk{
				Labels: map[string]string{
					BootDiskLabel: "true",
				},
			}, ""),
			want: "boot_disk",
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			if got := test.disk.DiskType(); got != test.want {
				t.Errorf("DiskType() = %v, want %v", got, test.want)
			}
		})
	}
}

func Test_UseStatus(t *testing.T) {
	tests := map[string]struct {
		disk *Disk
		want string
	}{
		"Disk with no users returns idle": {
			disk: NewDisk(&computev1.Disk{}, ""),
			want: idleDisk,
		},
		"Disk with users returns in-use": {
			disk: NewDisk(&computev1.Disk{
				Users: []string{"node-1", "node-2"},
			}, ""),
			want: inUseDisk,
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			if got := test.disk.UseStatus(); got != test.want {
				t.Errorf("UseStatus() = %v, want %v", got, test.want)
			}
		})
	}
}
