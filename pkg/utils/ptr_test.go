package utils

import "testing"

func TestStringValue(t *testing.T) {
	t.Run("nil pointer returns empty string", func(t *testing.T) {
		if got := StringValue(nil); got != "" {
			t.Fatalf("StringValue(nil) = %q, want empty string", got)
		}
	})

	t.Run("non-nil pointer returns value", func(t *testing.T) {
		if got := StringValue(StringPtr("hello")); got != "hello" {
			t.Fatalf("StringValue(StringPtr(\"hello\")) = %q, want %q", got, "hello")
		}
	})
}

func TestStringPtr(t *testing.T) {
	got := StringPtr("hello")
	if got == nil {
		t.Fatal("StringPtr returned nil")
	}
	if *got != "hello" {
		t.Fatalf("*StringPtr(\"hello\") = %q, want %q", *got, "hello")
	}
}
