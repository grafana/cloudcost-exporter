package config

import (
	"flag"
	"testing"
)

func TestStringSliceFlag_Set(t *testing.T) {
	var ssf StringSliceFlag
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	fs.Var(&ssf, "test", "test")
	if err := fs.Parse([]string{"-test", "test1", "-test", "test2"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if exp, got := 2, len(ssf); exp != got {
		t.Fatalf("expected %d, got %d", exp, got)
	}
}

func TestStringSliceFlag_String(t *testing.T) {
	var ssf StringSliceFlag
	for _, v := range []string{"test1", "test2"} {
		if err := ssf.Set(v); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	}
	if exp, got := "test1,test2", ssf.String(); exp != got {
		t.Fatalf("expected %q, got %q", exp, got)
	}
}
