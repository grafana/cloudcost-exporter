package client

import (
	"testing"
)

func Test_stripOutKeyFromDescription(t *testing.T) {
	tests := map[string]struct {
		description string
		want        string
	}{
		"simple": {
			description: "N1 Predefined Instance Core running in Americas",
			want:        "N1 Predefined Instance Core",
		},
		"commitment v1: empty": {
			description: "Commitment v1:",
			want:        "",
		},
		"commitment v1": {
			description: "Commitment v1: N2 Predefined Instance Core in Americas",
			want:        "N2 Predefined Instance Core",
		},
		"commitment v2": {
			description: "Commitment v1: N2D AMD Ram in Americin for 1 year",
			want:        "N2D AMD Ram",
		},
		"commitment berlin": {
			description: "Commitment v1: G2 Ram in Berlin for 1 year",
			want:        "G2 Ram",
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			if got := stripOutKeyFromDescription(tt.description); got != tt.want {
				t.Errorf("stripOutKeyFromDescription() = %v, want %v", got, tt.want)
			}
		})
	}
}
func Test_getMachineInfoFromMachineType(t *testing.T) {
	type result struct {
		wantCpu         int
		wantRam         int
		wantZone        string
		wantType        string
		wantMachineType string
	}
	tests := map[string]struct {
		machineType string
		want        result
	}{
		"simple": {
			machineType: "https://www.googleapis.com/compute/v1/projects/grafanalabs-dev/zones/us-central1-a/machineTypes/n2-standard-8",
			want: result{
				wantCpu:         2,
				wantRam:         8,
				wantZone:        "us-central1-a",
				wantMachineType: "n2-standard-8",
				wantType:        "n2",
			},
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			if got := getMachineTypeFromURL(test.machineType); got != test.want.wantMachineType {
				t.Errorf("getMachineTypeFromURL() = %v, want %v", got, test.want.wantMachineType)
			}
		})
	}
}

func Test_GetMachineFamily(t *testing.T) {
	tests := map[string]struct {
		machineType string
		want        string
	}{
		"n1": {
			machineType: "n1-standard-8",
			want:        "n1",
		},
		"n2": {
			machineType: "n2-standard-8",
			want:        "n2",
		},
		"n2-bad": {
			machineType: "n2_standard",
			want:        "",
		},
		"n2d": {
			machineType: "n2d-standard-8",
			want:        "n2d",
		},
		"e1": {
			machineType: "e2-standard-8",
			want:        "e2",
		},
		"g1": {
			machineType: "g1-standard-8",
			want:        "g1",
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			if got := getMachineFamily(test.machineType); got != test.want {
				t.Errorf("stripOutKeyFromDescription() = %v, want %v", got, test.want)
			}
		})
	}
}
