DOCKER_REPO ?= quay.io/
IMAGE ?= jmesnil/wildfly-operator
TAG ?= latest
PROG  := wildfly-operator

.PHONY: dep build image push run clean help

.DEFAULT_GOAL := help

## setup            Ensure the operator-sdk is installed.
setup:
	./build/setup-operator-sdk.sh

## dep              Ensure deps are locally available.
dep:
	dep ensure

## codegen          Ensure code is generated.
codegen: setup
	operator-sdk generate k8s
	operator-sdk generate openapi

## build            Compile and build the WildFly operator.
build: dep codegen unit-test
	operator-sdk build "${DOCKER_REPO}$(IMAGE):$(TAG)"

## push             Push Docker image to the Quay.io repository.
push: build
	docker push "${DOCKER_REPO}$(IMAGE):$(TAG)"

## clean            Remove all generated build files.
clean:
	rm -rf build/_output

## run-minikube     Run the WildFly operator on Minikube.
run-minikube:
	./build/run-minikube.sh

## run-openshift    Run the WildFly operator on OpenShift.
run-openshift:
	./build/run-openshift.sh

## test             Perform all tests.
test: unit-test scorecard test-e2e

## test-e2e         Run e2e test
test-e2e: setup
	operator-sdk test local ./test/e2e/

## scorecard        Run operator-sdk scorecard.
scorecard: setup
	operator-sdk scorecard --verbose

## unit-test        Perform unit tests.
unit-test:
	go test -v ./... -tags=unit

## release     Release a versioned operator.
##             - Requires 'RELEASE_NAME=X.Y.Z'. Defaults to dry run.
##             - Pass 'DRY_RUN=false' to commit the release.
##             - Example: "make DRY_RUN=false RELEASE_NAME=X.Y.Z release"
##
release:
	build/release.sh

help : Makefile
	@sed -n 's/^##//p' $<