.PHONY: build clean grafanactl-serve check-grafanactl

# Same as default in cmd/dashboards/main.go so root make build-dashboards and this Makefile share one output
OUTPUT_DIR := $(PWD)/grafana
GRAFANACTL_PORT ?= 8080

# Generate dashboards (writes manifests to grafana/ for grafanactl)
build:
	@echo "Generating dashboards..."
	@mkdir -p $(OUTPUT_DIR)
	@cd .. && go run ./cmd/dashboards/main.go --output=file --output-dir=$(OUTPUT_DIR)
	@echo "✅ Generated dashboards in $(OUTPUT_DIR)/"
	@echo ""
	@echo "To view them locally, run:"
	@echo "  make grafanactl-serve"

# Clean generated files
clean:
	@echo "Cleaning generated dashboards..."
	@rm -rf $(OUTPUT_DIR)
	@echo "✅ Cleaned $(OUTPUT_DIR)/"

# Serve dashboards locally with grafanactl
grafanactl-serve: check-grafanactl build
	@echo "Starting grafanactl server on port $(GRAFANACTL_PORT)..."
	@echo "View dashboards at: http://localhost:$(GRAFANACTL_PORT)"
	grafanactl resources serve --port $(GRAFANACTL_PORT) $(OUTPUT_DIR)

# Check if grafanactl is installed
check-grafanactl:
	@command -v grafanactl >/dev/null 2>&1 || { \
		echo "❌ Error: grafanactl is required but not installed."; \
		echo ""; \
		echo "Install it from: https://grafana.github.io/grafanactl/"; \
		echo ""; \
		echo "Or with Homebrew:"; \
		echo "  brew install grafana/grafana/grafanactl"; \
		exit 1; \
	}

help:
	@echo "CloudCost Exporter Dashboards - Makefile targets:"
	@echo ""
	@echo "  make build              - Generate dashboard JSON files"
	@echo "  make clean              - Remove generated dashboard files"
	@echo "  make grafanactl-serve   - Generate and serve dashboards locally (requires grafanactl)"
	@echo ""
	@echo "Environment variables:"
	@echo "  GRAFANACTL_PORT         - Port for grafanactl server (default: 8080)"
	@echo ""
	@echo "Examples:"
	@echo "  make build"
	@echo "  make grafanactl-serve"
	@echo "  make grafanactl-serve GRAFANACTL_PORT=3000"
