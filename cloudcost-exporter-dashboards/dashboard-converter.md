# Dashboard Converter

## Quick Start

### Using Docker

```bash
# Build the Docker image
cd grafana-foundation-sdk/scripts/dashboard-converter
docker build -t dashboard-converter:latest .

# Convert a dashboard using Docker
docker run -v $(pwd):/workspace -w /workspace dashboard-converter:latest \
  dashboard.json -o dashboard.go

# Or mount a specific directory
docker run -v /path/to/dashboards:/workspace -w /workspace dashboard-converter:latest \
  dashboard.json -o dashboard.go
```

### Using Local Build

```bash
# Clone and build
git clone https://github.com/grafana/grafana-foundation-sdk.git
cd grafana-foundation-sdk/scripts/dashboard-converter
make build

# Convert a dashboard
./build/dashboard-converter dashboard.json -o dashboard.go
```

See the [full documentation](https://github.com/grafana/grafana-foundation-sdk/tree/main/scripts/dashboard-converter) for usage details.
