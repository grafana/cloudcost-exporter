package config

import (
	"flag"
	"testing"
)

func TestStringSliceFlag_Set(t *testing.T) {
	tests := map[string]struct {
		values []string
		exp    int
	}{
		"empty": {
			values: []string{},
			exp:    0,
		},
		"single": {
			values: []string{"-test", "test1"},
			exp:    1,
		},
		"multiple": {
			values: []string{"-test", "test1", "-test", "test2"},
			exp:    2,
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			var ssf StringSliceFlag
			fs := flag.NewFlagSet("test", flag.ContinueOnError)
			fs.Var(&ssf, "test", "test")
			if err := fs.Parse(test.values); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if exp, got := test.exp, len(ssf); exp != got {
				t.Fatalf("expected %d, got %d", exp, got)
			}
		})
	}
}

func TestStringSliceFlag_String(t *testing.T) {
	tests := map[string]struct {
		values []string
		exp    string
	}{
		"empty": {
			values: []string{},
			exp:    "",
		}, "single": {
			values: []string{"test1"},
			exp:    "test1",
		}, "multiple": {
			values: []string{"test1", "test2"},
			exp:    "test1,test2",
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			var ssf StringSliceFlag
			for _, v := range test.values {
				if err := ssf.Set(v); err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
			}
			if exp, got := test.exp, ssf.String(); exp != got {
				t.Fatalf("expected %q, got %q", exp, got)
			}
		})
	}
}
