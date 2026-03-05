package client

import (
	"log/slog"
	"strings"
)

// RegionsForProjects fetches and deduplicates regions across all given GCP projects.
func RegionsForProjects(gcpClient Client, projects []string, logger *slog.Logger) []string {
	regionSet := make(map[string]struct{})
	for _, project := range projects {
		rs, err := gcpClient.GetRegions(project)
		if err != nil {
			logger.Warn("failed to get regions", "project", project, "error", err)
			continue
		}
		for _, r := range rs {
			regionSet[r.Name] = struct{}{}
		}
	}
	regions := make([]string, 0, len(regionSet))
	for r := range regionSet {
		regions = append(regions, r)
	}
	return regions
}

// RegionsFromZonesForProjects fetches zones and derives region names
// (e.g. "us-central1-a" → "us-central1") across all given GCP projects.
func RegionsFromZonesForProjects(gcpClient Client, projects []string, logger *slog.Logger) []string {
	regionSet := make(map[string]struct{})
	for _, project := range projects {
		zones, err := gcpClient.GetZones(project)
		if err != nil {
			logger.Warn("failed to get zones", "project", project, "error", err)
			continue
		}
		for _, z := range zones {
			parts := strings.Split(z.Name, "-")
			if len(parts) >= 3 {
				regionSet[strings.Join(parts[:len(parts)-1], "-")] = struct{}{}
			}
		}
	}
	regions := make([]string, 0, len(regionSet))
	for r := range regionSet {
		regions = append(regions, r)
	}
	return regions
}
