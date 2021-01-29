DOCKER_REPO ?= quay.io/wildfly/
IMAGE ?= wildfly-operator
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

## vendor                Ensure vendor modules are updated.
vendor: tidy
	go mod vendor

## codegen               Ensure code is generated.
codegen: setup
    # see https://github.com/operator-framework/operator-sdk/issues/1854#issuecomment-525132306
	GOROOT="$(shell dirname `which go`)" ./operator-sdk generate k8s
	./operator-sdk generate crds
	which ./openapi-gen > /dev/null || go build -o ./openapi-gen k8s.io/kube-openapi/cmd/openapi-gen
	./openapi-gen --logtostderr=true -o "" -i ./pkg/apis/wildfly/v1alpha1 -O zz_generated.openapi -p ./pkg/apis/wildfly/v1alpha1 -h ./hack/boilerplate.go.txt -r "-"

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
	kubectl apply -f deploy/crds/wildfly.org_wildflyservers_crd.yaml
	JBOSS_HOME=/wildfly JBOSS_BOOTABLE_DATA_DIR=/opt/jboss/container/wildfly-bootable-jar-data JBOSS_BOOTABLE_HOME=/opt/jboss/container/wildfly-bootable-jar-server OPERATOR_NAME=wildfly-operator ./operator-sdk run --local --operator-namespace=default --verbose --operator-flags "--zap-devel --zap-level=5"

## test                  Perform all tests.
test: unit-test scorecard test-e2e

## test-e2e-local        Run e2e tests with a local operator
test-e2e-local: setup
	LOCAL_OPERATOR=true JBOSS_HOME=/wildfly JBOSS_BOOTABLE_DATA_DIR=/opt/jboss/container/wildfly-bootable-jar-data JBOSS_BOOTABLE_HOME=/opt/jboss/container/wildfly-bootable-jar-server OPERATOR_NAME=wildfly-operator ./operator-sdk test local ./test/e2e --verbose --debug  --operator-namespace default --up-local --local-operator-flags "--zap-devel --zap-level=5" --global-manifest ./deploy/crds/wildfly.org_wildflyservers_crd.yaml

push-to-minikube-image-registry:
	docker run -d -p 5000:5000 --restart=always --name image-registry registry || true
	DOCKER_REPO="localhost:5000/" IMAGE="wildfly-operator" make push

## test-e2e-minikube     Run e2e tests with a containerized operator in Minikube
test-e2e-minikube: setup push-to-minikube-image-registry
	./operator-sdk test local ./test/e2e --verbose --debug  --operator-namespace default --global-manifest ./deploy/crds/wildfly.org_wildflyservers_crd.yaml --namespaced-manifest ./deploy/operator.yaml --image "localhost:5000/wildfly-operator:latest"

## scorecard             Run operator-sdk scorecard.
scorecard: setup
	./operator-sdk scorecard --verbose

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
