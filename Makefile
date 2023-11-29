.PHONY: build-image build-binary build test push push-dev

current_makefile_dir := $(dir $(abspath $(lastword $(MAKEFILE_LIST))))
include $(current_makefile_dir)/../common.inc

IMAGE_NAME=cloudcost-exporter
IMAGE_NAME_LATEST=${IMAGE_PREFIX}/${IMAGE_NAME}:latest
IMAGE_NAME_VERSION=$(IMAGE_PREFIX)/$(IMAGE_NAME):$(VERSION)

WORKFLOW_TEMPLATE=cloudcost-exporter
WORKFLOW_NAMESPACE=capacity-cd

build-image:
	docker build --build-arg GO_LDFLAGS="$(GO_LDFLAGS)" -t $(IMAGE_PREFIX)/$(IMAGE_NAME) -t $(IMAGE_NAME_VERSION) .

build-binary:
	go build -v -ldflags "$(GO_LDFLAGS)" -o cloudcost-exporter ./cmd/exporter

build: build-binary build-image

test: build
	go test -v ./...

push-dev: build test
	docker push $(IMAGE_NAME_VERSION)

push: build test push-dev
	docker push $(IMAGE_NAME_LATEST)
