package utils

import (
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
)

func TestGenerateDesc(t *testing.T) {
	prefix := "test_prefix"
	subsystem := "test_subsystem"
	suffix := "test_suffix"
	description := "This is a test description"
	labels := []string{"label1", "label2"}

	desc := GenerateDesc(prefix, subsystem, suffix, description, labels)

	// Expected values
	expectedFQName := prometheus.BuildFQName(prefix, subsystem, suffix)

	if !strings.Contains(desc.String(), expectedFQName) {
		t.Errorf("Expected FQName %s in desc, but got %s", expectedFQName, desc.String())
	}

	if !strings.Contains(desc.String(), description) {
		t.Errorf("Expected description %s in desc, but got %s", description, desc.String())
	}

	for _, label := range labels {
		if !strings.Contains(desc.String(), label) {
			t.Errorf("Expected label %s in desc, but got %s", label, desc.String())
		}
	}
}
