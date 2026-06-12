// Package servicesdrift verifies that docs/metrics/README.md stays in sync with
// the per-provider Services() registries. Adding a service to a provider but
// forgetting to document it (or vice versa) makes this test fail.
package servicesdrift

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/grafana/cloudcost-exporter/pkg/aws"
	"github.com/grafana/cloudcost-exporter/pkg/azure"
	"github.com/grafana/cloudcost-exporter/pkg/google"
	"github.com/grafana/cloudcost-exporter/pkg/provider"
)

// readmeRelPath is resolved from this test file's directory: internal/servicesdrift
// up two levels to the repo root, then into docs/metrics.
const readmeRelPath = "../../docs/metrics/README.md"

// backtickToken extracts every backtick-wrapped token on a line.
// e.g. "(`MANAGEDKAFKA`, alias: `KAFKA`)" yields ["MANAGEDKAFKA", "KAFKA"].
var backtickToken = regexp.MustCompile("`([^`]+)`")

// sectionHeader matches "## AWS Services" / "## GCP Services" / "## Azure Services".
var sectionHeader = regexp.MustCompile(`^## (AWS|GCP|Azure) Services\s*$`)

func TestReadmeMatchesRegistries(t *testing.T) {
	readmePath, err := filepath.Abs(readmeRelPath)
	if err != nil {
		t.Fatalf("resolve README path: %v", err)
	}
	raw, err := os.ReadFile(readmePath)
	if err != nil {
		t.Fatalf("read %s: %v", readmePath, err)
	}

	docTokens := parseSections(t, string(raw))

	cases := []struct {
		provider string
		services []provider.ServiceInfo
	}{
		{"AWS", aws.Services()},
		{"GCP", google.Services()},
		{"Azure", azure.Services()},
	}

	for _, tc := range cases {
		t.Run(tc.provider, func(t *testing.T) {
			doc, ok := docTokens[tc.provider]
			if !ok {
				t.Fatalf("docs/metrics/README.md missing '## %s Services' section", tc.provider)
			}
			registry := registryTokens(tc.services)
			missingFromDoc := setDiff(registry, doc)
			extraInDoc := setDiff(doc, registry)
			if len(missingFromDoc) > 0 {
				t.Errorf("%s: registry has %v, docs/metrics/README.md is missing them", tc.provider, missingFromDoc)
			}
			if len(extraInDoc) > 0 {
				t.Errorf("%s: docs/metrics/README.md has %v, registry doesn't (remove or add to Services())", tc.provider, extraInDoc)
			}
		})
	}
}

// parseSections walks the README line by line and returns, per provider header,
// the set of backtick-wrapped tokens found in bullet lines under that section.
func parseSections(t *testing.T, body string) map[string]map[string]struct{} {
	t.Helper()
	result := map[string]map[string]struct{}{}
	var current string
	for line := range strings.SplitSeq(body, "\n") {
		if m := sectionHeader.FindStringSubmatch(line); m != nil {
			current = m[1]
			result[current] = map[string]struct{}{}
			continue
		}
		if current == "" {
			continue
		}
		if strings.HasPrefix(line, "## ") {
			current = ""
			continue
		}
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "- ") {
			continue
		}
		for _, m := range backtickToken.FindAllStringSubmatch(trimmed, -1) {
			result[current][m[1]] = struct{}{}
		}
	}
	for _, p := range []string{"AWS", "GCP", "Azure"} {
		if _, ok := result[p]; !ok {
			t.Fatalf("docs/metrics/README.md is missing '## %s Services' header (parser may be broken)", p)
		}
	}
	return result
}

func registryTokens(services []provider.ServiceInfo) map[string]struct{} {
	out := map[string]struct{}{}
	for _, s := range services {
		out[s.Name] = struct{}{}
		for _, a := range s.Aliases {
			out[a] = struct{}{}
		}
	}
	return out
}

func setDiff(a, b map[string]struct{}) []string {
	var diff []string
	for k := range a {
		if _, ok := b[k]; !ok {
			diff = append(diff, k)
		}
	}
	sort.Strings(diff)
	return diff
}
