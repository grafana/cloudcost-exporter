package provider

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestMergeServiceEntries(t *testing.T) {
	tests := []struct {
		name         string
		stable       []string
		experimental []string
		want         []ServiceEntry
	}{
		{
			name: "empty inputs (unset flags round-trip as [\"\"])",
			// strings.Split("", ",") yields [""], so empties must be dropped.
			stable:       []string{""},
			experimental: []string{""},
			want:         []ServiceEntry{},
		},
		{
			name:   "stable only",
			stable: []string{"EC2", "S3"},
			want:   []ServiceEntry{{Name: "EC2"}, {Name: "S3"}},
		},
		{
			name:         "experimental flagged and ordered after stable",
			stable:       []string{"EC2"},
			experimental: []string{"bedrock", "vertex"},
			want: []ServiceEntry{
				{Name: "EC2"},
				{Name: "bedrock", Experimental: true},
				{Name: "vertex", Experimental: true},
			},
		},
		{
			name:         "experimental only",
			experimental: []string{"bedrock"},
			want:         []ServiceEntry{{Name: "bedrock", Experimental: true}},
		},
		{
			name:         "service enabled as stable is not re-registered as experimental (case-insensitive)",
			stable:       []string{"BEDROCK"},
			experimental: []string{"bedrock"},
			want:         []ServiceEntry{{Name: "BEDROCK"}},
		},
		{
			name:         "names are trimmed",
			stable:       []string{" EC2 "},
			experimental: []string{" bedrock "},
			want:         []ServiceEntry{{Name: "EC2"}, {Name: "bedrock", Experimental: true}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, MergeServiceEntries(tt.stable, tt.experimental))
		})
	}
}
