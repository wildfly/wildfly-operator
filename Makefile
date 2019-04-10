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

init-test: setup
	mkdir -p build/_output
	cat deploy/rbac.yaml > build/_output/rbac-and-operator.yaml
	echo "\n---\n" >> build/_output/rbac-and-operator.yaml
	cat deploy/operator.yaml >> build/_output/rbac-and-operator.yaml

## test             Perform all tests.
test: unit-test scorecard test-e2e

## test-e2e         Run e2e test
test-e2e: init-test
	operator-sdk test local --namespaced-manifest build/_output/rbac-and-operator.yaml --global-manifest deploy/crd.yaml ./test/e2e/

## scorecard        Run operator-sdk scorecard.
scorecard: init-test
	operator-sdk scorecard --verbose

## unit-test        Perform unit tests.
unit-test:
	go test -v ./... -tags=unit

help : Makefile
	@sed -n 's/^##//p' $<