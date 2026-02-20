.PHONY: build-image build-binary build test push push-dev generate help

help:
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-30s\033[0m %s\n", $$1, $$2}'

VERSION=$(shell git describe --tags --dirty --always)

IMAGE_PREFIX=grafana

IMAGE_NAME=cloudcost-exporter
IMAGE_NAME_LATEST=${IMAGE_PREFIX}/${IMAGE_NAME}:latest
IMAGE_NAME_VERSION=$(IMAGE_PREFIX)/$(IMAGE_NAME):$(VERSION)

WORKFLOW_TEMPLATE=cloudcost-exporter

PROM_VERSION_PKG ?= github.com/prometheus/common/version
BUILD_USER   ?= $(shell whoami)@$(shell hostname)
BUILD_DATE   ?= $(shell date -u +"%Y-%m-%dT%H:%M:%SZ")
GIT_BRANCH   ?= $(shell git rev-parse --abbrev-ref HEAD)
GIT_REVISION ?= $(shell git rev-parse --short HEAD)
GO_LDFLAGS = -X $(PROM_VERSION_PKG).Branch=$(GIT_BRANCH) -X $(PROM_VERSION_PKG).Version=$(VERSION) -X $(PROM_VERSION_PKG).Revision=$(GIT_REVISION) -X ${PROM_VERSION_PKG}.BuildUser=${BUILD_USER} -X ${PROM_VERSION_PKG}.BuildDate=${BUILD_DATE}

build-image: ## Build the Docker image
	docker build --build-arg GO_LDFLAGS="$(GO_LDFLAGS)" -t $(IMAGE_PREFIX)/$(IMAGE_NAME) -t $(IMAGE_NAME_VERSION) .

build-binary: ## Compile the binary
	CGO_ENABLED=0 go build -v -ldflags "$(GO_LDFLAGS)" -o cloudcost-exporter ./cmd/exporter

build: lint generate build-binary build-image ## Run lint, generate, and build binary and image

generate: ## Run go generate
	go generate -v ./...

test: lint generate build-dashboards build ## Run the full test suite
	go test -v ./...

lint: ## Run linter over the codebase
	golangci-lint run ./...

push-dev: build test ## Build, test, and push the versioned image
	docker push $(IMAGE_NAME_VERSION)

push: build test push-dev ## Build, test, and push both versioned and latest images
	docker push $(IMAGE_NAME_LATEST)

grafanactl-serve: check-cli-grafanactl build-dashboards ## Serve dashboards locally with grafanactl
  grafanactl resources serve --port 8080 ./cloudcost-exporter-dashboards/grafana

build-dashboards: ## Build Grafana dashboards using grafana-foundation-sdk
	go get github.com/grafana/grafana-foundation-sdk/go@latest
	go mod tidy
	go run ./cmd/dashboards/main.go  --output=file

# Check for required CLI tools
check-cli-grafanactl: ERR_MSG := "grafanactl is required. Install it from https://grafana.github.io/grafanactl/"
check-cli-%:
	@command -v $(*) > /dev/null || { echo $(ERR_MSG); exit 1; }
