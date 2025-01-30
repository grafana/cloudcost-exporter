package main

import (
	"testing"
)

func Test_sluggify(t *testing.T) {
	tests := map[string]struct {
		s    string
		want string
	}{
		"empty string should return gracefully":         {s: "", want: ""},
		"single word should return as is":               {s: "word", want: "word"},
		"multiple words should be hyphenated":           {s: "multiple words", want: "multiple-words"},
		"mixed case should be lowercased":               {s: "Mixed Case", want: "mixed-case"},
		"leading and trailing spaces should be removed": {s: "  leading and trailing spaces  ", want: "leading-and-trailing-spaces"},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			if got := sluggify(tt.s); got != tt.want {
				t.Errorf("sluggify() = %v, want %v", got, tt.want)
			}
		})
	}
}
