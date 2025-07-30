.PHONY: build-image build-binary build test push push-dev

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

build-image:
	docker build --build-arg GO_LDFLAGS="$(GO_LDFLAGS)" -t $(IMAGE_PREFIX)/$(IMAGE_NAME) -t $(IMAGE_NAME_VERSION) .

build-binary:
	CGO_ENABLED=0 go build -v -ldflags "$(GO_LDFLAGS)" -o cloudcost-exporter ./cmd/exporter

build: build-binary build-image

test: build
	go test -v ./...

lint:
	golangci-lint run ./...

push-dev: build test
	docker push $(IMAGE_NAME_VERSION)

push: build test push-dev
	docker push $(IMAGE_NAME_LATEST)

grizzly-serve:
	grr serve -p 8088 -w -S "go run ./cloudcost-exporter-dashboards/main.go"

build-dashboards:
	go run ./cmd/dashboards/main.go  --output=file
