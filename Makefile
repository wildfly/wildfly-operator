DOCKER_REPO ?= quay.io/
IMAGE ?= wildfly/wildfly-operator
TAG ?= latest
PROG  := wildfly-operator

.PHONY: dep build image push run clean help

.DEFAULT_GOAL := help

## setup                 Ensure the operator-sdk is installed.
setup:
	./build/setup-operator-sdk.sh

## tidy                  Ensure modules are tidy.
tidy:
	go mod tidy

## codegen               Ensure code is generated.
codegen: setup
	operator-sdk generate k8s
	operator-sdk generate openapi

## build                 Compile and build the WildFly operator.
build: tidy unit-test
	./build/build.sh ${GOOS}

## image                 Create the Docker image of the operator
image: build
	docker build -t "${DOCKER_REPO}$(IMAGE):$(TAG)" . -f build/Dockerfile

## push                  Push Docker image to the Quay.io repository.
push: image
	docker push "${DOCKER_REPO}$(IMAGE):$(TAG)"

## clean                 Remove all generated build files.
clean:
	rm -rf build/_output

## run-minikube          Run the WildFly operator on Minikube.
run-minikube:
	./build/run-minikube.sh

## run-openshift         Run the WildFly operator on OpenShift.
run-openshift:
	./build/run-openshift.sh

## run-local-operator    Run the operator locally (and not inside Kubernetes)
run-local-operator: codegen build
	echo "Deploy WildFlyServer CRD on Kubernetes"
	kubectl apply -f deploy/crds/wildfly_v1alpha1_wildflyserver_crd.yaml
	JBOSS_HOME=/wildfly OPERATOR_NAME=wildfly-operator operator-sdk up local --namespace=default

## test                  Perform all tests.
test: unit-test scorecard test-e2e

## test-e2e-local        Run e2e tests locally
test-e2e: test-e2e-17-local test-e2e-17-local

## test-e2e-17-local     Run e2e test for WildFly 17.0 with a local operator
test-e2e-17-local: setup
	LOCAL_OPERATOR=true JBOSS_HOME=/wildfly OPERATOR_NAME=wildfly-operator operator-sdk test local ./test/e2e/17.0 --verbose --debug  --namespace default --up-local

## test-e2e-18-local     Run e2e test for WildFly 18.0 with a local operator
test-e2e-18-local: setup
	LOCAL_OPERATOR=true JBOSS_HOME=/wildfly OPERATOR_NAME=wildfly-operator operator-sdk test local ./test/e2e/18.0 --verbose --debug  --namespace default --up-local

## test-e2e-18           Run e2e test for WildFly 18.0 with a containerized operator
test-e2e-18: setup
	operator-sdk test local ./test/e2e/18.0 --verbose --debug

## scorecard             Run operator-sdk scorecard.
scorecard: setup
	operator-sdk scorecard --verbose

## unit-test             Perform unit tests.
unit-test:
	go test -v ./... -tags=unit

## release               Release a versioned operator.
##                       - Requires 'RELEASE_NAME=X.Y.Z'. Defaults to dry run.
##                       - Pass 'DRY_RUN=false' to commit the release.
##                       Example: "make DRY_RUN=false RELEASE_NAME=X.Y.Z release"
##
release:
	build/release.sh

help : Makefile
	@sed -n 's/^##//p' $<