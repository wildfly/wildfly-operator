DOCKER_REPO ?= quay.io/
IMAGE ?= jmesnil/wildfly-operator
TAG ?= latest
PROG  := wildfly-operator

.PHONY: dep build image push run clean help

.DEFAULT_GOAL := help

## setup      Ensure the operator-sdk is installed.
setup:
	./build/setup-operator-sdk.sh

## dep         Ensure deps is locally available.
dep:
	dep ensure

## codegen     Ensure code is generated.
codegen: setup
	operator-sdk generate k8s
	operator-sdk generate openapi

## build       Compile and build the Infinispan operator.
build: dep codegen unit-test
	operator-sdk build "${DOCKER_REPO}$(IMAGE):$(TAG)"

## push        Push Docker images to Docker Hub.
push: build
	docker push "${DOCKER_REPO}$(IMAGE):$(TAG)"

## clean       Remove all generated build files.
clean:
	rm -rf build/_output

## scorecard   Run operator-sdk scorecard
scorecard: dep init
	operator-sdk scorecard

## unit-test   Perform unit tests
unit-test: dep
	go test -v ./...

## test        Perform all tests
test: unit-test scorecard

help : Makefile
	@sed -n 's/^##//p' $<