.PHONY: build-image build-binary build test push push-dev

current_makefile_dir := $(dir $(abspath $(lastword $(MAKEFILE_LIST))))

# deployment_tools/docker/image-tag.sh
VERSION=dev-$(shell date +%Y-%m-%d)-$(shell git rev-parse --short HEAD)

# deployment_tools/docker/common.inc
IMAGE_PREFIX=us.gcr.io/kubernetes-dev

IMAGE_NAME=cloudcost-exporter
IMAGE_NAME_LATEST=${IMAGE_PREFIX}/${IMAGE_NAME}:latest
IMAGE_NAME_VERSION=$(IMAGE_PREFIX)/$(IMAGE_NAME):$(VERSION)

WORKFLOW_TEMPLATE=cloudcost-exporter
WORKFLOW_NAMESPACE=capacity-cd

PROM_VERSION_PKG ?= github.com/prometheus/common/version
BUILD_USER   ?= $(shell whoami)@$(shell hostname)
BUILD_DATE   ?= $(shell date -u +"%Y-%m-%dT%H:%M:%SZ")
GIT_BRANCH   ?= $(shell git rev-parse --abbrev-ref HEAD)
GIT_REVISION ?= $(shell git rev-parse --short HEAD)
GO_LDFLAGS = -X $(PROM_VERSION_PKG).Branch=$(GIT_BRANCH) -X $(PROM_VERSION_PKG).Version=$(VERSION) -X $(PROM_VERSION_PKG).Revision=$(GIT_REVISION) -X ${PROM_VERSION_PKG}.BuildUser=${BUILD_USER} -X ${PROM_VERSION_PKG}.BuildDate=${BUILD_DATE}

build-image:
	docker build --build-arg GO_LDFLAGS="$(GO_LDFLAGS)" -t $(IMAGE_PREFIX)/$(IMAGE_NAME) -t $(IMAGE_NAME_VERSION) .

build-binary:
	go build -v -ldflags "$(GO_LDFLAGS)" -o cloudcost-exporter ./cmd/exporter

build: build-binary build-image

test: build
	go test -v ./...

lint:
	golangci-lint run ./...

push-dev: build test
	docker push $(IMAGE_NAME_VERSION)

push: build test push-dev
	docker push $(IMAGE_NAME_LATEST)
