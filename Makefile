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
