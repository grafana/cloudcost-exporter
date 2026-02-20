# Grafana Dashboards

This contains a set of Grafana Dashboards that are generated using the [Grafana Foundation SDK](https://github.com/grafana/grafana-foundation-sdk).

## Getting Started

### Prerequisites

- Go 1.21+ installed
- `grafanactl` CLI tool for local preview ([install guide](https://grafana.github.io/grafanactl/))

### Generate & Preview Dashboards

```shell
# Generate dashboard files
make build-dashboards

# Preview dashboards locally:
make grafanactl-serve
```

Access dashboards at http://localhost:8080

The dashboard files are served from `cloudcost-exporter-dashboards/grafana/`.
