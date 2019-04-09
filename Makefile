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
test: unit-test scorecard

## scorecard        Run operator-sdk scorecard.
scorecard: dep setup
	cat deploy/rbac.yaml > build/_output/rbac-and-operator.yaml
	echo "\n---\n" >> build/_output/rbac-and-operator.yaml
	cat deploy/operator.yaml >> build/_output/rbac-and-operator.yaml
	operator-sdk scorecard

## unit-test        Perform unit tests.
unit-test: dep
	go test -v ./...

help : Makefile
	@sed -n 's/^##//p' $<