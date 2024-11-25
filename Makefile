# VERSION defines the project version for the bundle.
# Update this value when you upgrade the version of your project.
# To re-generate a bundle for another specific version without changing the standard setup, you can:
# - use the VERSION as arg of the bundle target (e.g make bundle VERSION=0.0.2)
# - use environment variables to overwrite this value (e.g export VERSION=0.0.2)
VERSION ?= 1.1.2

# The higher Go version expected to be used
REQUIRED_GO_MAJOR_VERSION := 1
REQUIRED_GO_MINOR_VERSION := 21
REQUIRED_GO_VERSION := $(REQUIRED_GO_MAJOR_VERSION).$(REQUIRED_GO_MINOR_VERSION)

# Check if Go is installed
ifeq (, $(shell which go))
$(error "Go is not installed. Please install Go $(REQUIRED_GO_VERSION).")
endif

CURRENT_GO_MAJOR_VERSION := $(shell go version | sed -n 's/^go version go\([0-9]*\).*/\1/p')
CURRENT_GO_MINOR_VERSION := $(shell go version | sed -n 's/^go version go$(CURRENT_GO_MAJOR_VERSION).\([0-9]*\).*/\1/p')
CURRENT_GO_VERSION := $(CURRENT_GO_MAJOR_VERSION).$(CURRENT_GO_MINOR_VERSION)
$(info Using Go version $(CURRENT_GO_VERSION))
ifeq ($(shell expr $(CURRENT_GO_MAJOR_VERSION) \> $(REQUIRED_GO_MAJOR_VERSION)), 1)
$(error "Go $(REQUIRED_GO_VERSION) or below is required. Current version is: $(CURRENT_GO_VERSION)")
endif
ifeq ($(shell expr $(CURRENT_GO_MINOR_VERSION) \> $(REQUIRED_GO_MINOR_VERSION)), 1)
$(error "Go $(REQUIRED_GO_VERSION) or below is required. Current version is: $(CURRENT_GO_VERSION)")
endif

# CHANNELS define the bundle channels used in the bundle.
# Add a new line here if you would like to change its default config. (E.g CHANNELS = "candidate,fast,stable")
# To re-generate a bundle for other specific channels without changing the standard setup, you can:
# - use the CHANNELS as arg of the bundle target (e.g make bundle CHANNELS=candidate,fast,stable)
# - use environment variables to overwrite this value (e.g export CHANNELS="candidate,fast,stable")
ifneq ($(origin CHANNELS), undefined)
BUNDLE_CHANNELS := --channels=$(CHANNELS)
endif

# DEFAULT_CHANNEL defines the default channel used in the bundle.
# Add a new line here if you would like to change its default config. (E.g DEFAULT_CHANNEL = "stable")
# To re-generate a bundle for any other default channel without changing the default setup, you can:
# - use the DEFAULT_CHANNEL as arg of the bundle target (e.g make bundle DEFAULT_CHANNEL=stable)
# - use environment variables to overwrite this value (e.g export DEFAULT_CHANNEL="stable")
ifneq ($(origin DEFAULT_CHANNEL), undefined)
BUNDLE_DEFAULT_CHANNEL := --default-channel=$(DEFAULT_CHANNEL)
endif
BUNDLE_METADATA_OPTS ?= $(BUNDLE_CHANNELS) $(BUNDLE_DEFAULT_CHANNEL)

# IMAGE_TAG_BASE defines the docker.io namespace and part of the image name for remote images.
# This variable is used to construct full image tags for bundle and catalog images.
#
# For example, running 'make bundle-build bundle-push catalog-build catalog-push' will build and push both
# org/wildfly-operator-upgrade-bundle:$VERSION and org/wildfly-operator-upgrade-catalog:$VERSION.
IMAGE_TAG_BASE ?= quay.io/wildfly/wildfly-operator

# BUNDLE_IMG defines the image:tag used for the bundle.
# You can use it as an arg. (E.g make bundle-build BUNDLE_IMG=<some-registry>/<project-name-bundle>:<tag>)
BUNDLE_IMG ?= $(IMAGE_TAG_BASE)-bundle:v$(VERSION)

# BUNDLE_GEN_FLAGS are the flags passed to the operator-sdk generate bundle command
BUNDLE_GEN_FLAGS ?= -q --overwrite --version $(VERSION) $(BUNDLE_METADATA_OPTS)

# USE_IMAGE_DIGESTS defines if images are resolved via tags or digests
# You can enable this value if you would like to use SHA Based Digests
# To enable set flag to true
USE_IMAGE_DIGESTS ?= false
ifeq ($(USE_IMAGE_DIGESTS), true)
	BUNDLE_GEN_FLAGS += --use-image-digests
endif

# Set the Operator SDK version to use. By default, what is installed on the system is used.
# This is useful for CI or a project to utilize a specific version of the operator-sdk toolkit.
OPERATOR_SDK_VERSION ?= v1.25.4

# Image URL to use all building/pushing image targets
IMG ?= quay.io/wildfly/wildfly-operator:latest
# ENVTEST_K8S_VERSION refers to the version of kubebuilder assets to be downloaded by envtest binary.
ENVTEST_K8S_VERSION = 1.25

# Get the currently used golang install path (in GOPATH/bin, unless GOBIN is set)
ifeq (,$(shell go env GOBIN))
GOBIN=$(shell go env GOPATH)/bin
else
GOBIN=$(shell go env GOBIN)
endif

# This is the test name space, it is used in this makefile to allow clean it up by using make clean
define TEST_NAMESPACE_YAML
kind: Namespace
apiVersion: v1
metadata:
  name: wildfly-op-test-ns
  labels:
    name:  wildfly-op-test-ns
endef

# Setting SHELL to bash allows bash commands to be executed by recipes.
# This is a requirement for 'setup-envtest.sh' in the test target.
# Options are set to exit when a recipe line exits non-zero or a piped command fails.
SHELL = /usr/bin/env bash -o pipefail
.SHELLFLAGS = -ec

.PHONY: all
all: help

##@ General

# The help target prints out all targets with their descriptions organized
# beneath their categories. The categories are represented by '##@' and the
# target descriptions by '##'. The awk commands is responsible for reading the
# entire set of makefiles included in this invocation, looking for lines of the
# file as xyz: ## something, and then pretty-format the target and help. Then,
# if there's a line with ##@ something, that gets pretty-printed as a category.
# More info on the usage of ANSI control characters for terminal formatting:
# https://en.wikipedia.org/wiki/ANSI_escape_code#SGR_parameters
# More info on the awk command:
# http://linuxcommand.org/lc3_adv_awk.php

.PHONY: help
help: ## Display this help.
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n"} /^[a-zA-Z_0-9-]+:.*?##/ { printf "  \033[36m%-15s\033[0m %s\n", $$1, $$2 } /^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) } ' $(MAKEFILE_LIST)

##@ Development

.PHONY: manifests
manifests: controller-gen ## Generate WebhookConfiguration, ClusterRole and CustomResourceDefinition objects.
	$(CONTROLLER_GEN) rbac:roleName=manager-role crd webhook paths="./..." output:crd:artifacts:config=config/crd/bases

.PHONY: generate
generate: controller-gen ## Generate code containing DeepCopy, DeepCopyInto, and DeepCopyObject method implementations.
	$(CONTROLLER_GEN) object:headerFile="hack/boilerplate.go.txt" paths="./..."

.PHONY: fmt
fmt: ## Run go fmt against code.
	go fmt ./...

.PHONY: vet
vet: ## Run go vet against code.
	go vet ./...

.PHONY: unit-test
unit-test: generate openapi fmt vet ## Run unit-tests.
	go test -v $(shell go list ./... | grep -v /test/) -coverprofile cover.out

.PHONY: test # Needs a cluster running and with a admin user logged in
test: clean manifests generate fmt vet controller-gen ## Run tests.
	go test -v ./test/e2e/... -v -ginkgo.v

.PHONY: debug-test # Needs a cluster running and with a admin user logged in
debug-test: clean manifests generate fmt vet controller-gen dlv ## Run tests.
	./bin/dlv test --listen=:2345 --headless=true --api-version=2 --accept-multiclient ./test/e2e/...

.PHONY: test-e2e
test-e2e: prepare-test-e2e run-test-e2e ## Used By CI.

## Run E2E tests running the Operator as a Deployment inside a local minikube cluster installation.
.PHONY: test-e2e-minikube
test-e2e-minikube: prepare-test-e2e
	docker run -d -p 5000:5000 --restart=always --name image-registry registry || true
	IMG="localhost:5000/wildfly-operator:latest" make docker-build docker-push run-test-e2e

.PHONY: prepare-test-e2e
prepare-test-e2e: manifests generate fmt vet kustomize

.PHONY: run-test-e2e
run-test-e2e:
	cd config/manager && $(KUSTOMIZE) edit set image controller=${IMG}
	$(KUSTOMIZE) build config/rbac | kubectl apply -f -
	mkdir -p dry-run
	$(KUSTOMIZE) build config/tests > dry-run/test-resources.yaml
	LOCAL_MANAGER=0 go test -timeout 20m ./test/e2e/... -v -ginkgo.v
	$(KUSTOMIZE) build config/rbac | kubectl delete --ignore-not-found=true -f -

.PHONY: clean
export TEST_NAMESPACE_YAML
clean: kustomize
	$(KUSTOMIZE) build config/rbac | kubectl delete --ignore-not-found=true -f -
	echo "$$TEST_NAMESPACE_YAML" | kubectl delete --ignore-not-found=true -f -

.PHONY: openapi
openapi: openapi-gen ## Generate the OpenAPI.
	$(OPENAPI_GEN) --logtostderr=true -o "" -i ./api/v1alpha1 -O zz_generated.openapi -p ./api/v1alpha1 -h ./hack/boilerplate.go.txt -r "-"

##@ Build

.PHONY: build
build: manifests generate openapi fmt vet ## Build manager binary.
	go build -o bin/manager main.go

.PHONY: run
run: manifests generate openapi fmt vet ## Run a controller from your host.
	JBOSS_HOME=/opt/wildfly \
JBOSS_BOOTABLE_DATA_DIR=/opt/jboss/container/wildfly-bootable-jar-data \
OPERATOR_NAME=wildfly-operator \
JBOSS_BOOTABLE_HOME=/opt/jboss/container/wildfly-bootable-jar-server \
go run ./main.go

.PHONY: docker-build
docker-build: unit-test ## Build docker image with the manager.
	./build/build.sh ${IMG}

.PHONY: tidy
tidy: ## Download any require dependency, clean up modules and refresh go.sum
	go mod tidy

.PHONY: vendor
vendor:  tidy ## Add missing and remove unused modules and make vendored copy of dependencies
	go mod vendor

# Run the manager with debug enabled
debug: dlv build
	JBOSS_HOME=/opt/wildfly JBOSS_BOOTABLE_DATA_DIR=/opt/jboss/container/wildfly-bootable-jar-data \
JBOSS_BOOTABLE_HOME=/opt/jboss/container/wildfly-bootable-jar-server OPERATOR_NAME=wildfly-operator  \
./bin/dlv --listen=:2345 --headless=true --api-version=2 --accept-multiclient exec bin/manager

# Generate the manifests in a directory
.PHONY: dry-run
dry-run: manifests kustomize
	cd config/manager && $(KUSTOMIZE) edit set image controller=${IMG}
	mkdir -p dry-run
	$(KUSTOMIZE) build config/default > dry-run/manifests.yaml

.PHONY: docker-push
docker-push: ## Push docker image with the manager.
	docker push ${IMG}

##@ Deployment

ifndef ignore-not-found
  ignore-not-found = false
endif

.PHONY: install
install: manifests kustomize ## Install CRDs into the K8s cluster specified in ~/.kube/config.
	$(KUSTOMIZE) build config/crd | kubectl apply -f -

.PHONY: uninstall
uninstall: manifests kustomize ## Uninstall CRDs from the K8s cluster specified in ~/.kube/config. Call with ignore-not-found=true to ignore resource not found errors during deletion.
	$(KUSTOMIZE) build config/crd | kubectl delete --ignore-not-found=$(ignore-not-found) -f -

.PHONY: deploy
deploy: manifests kustomize ## Deploy controller to the K8s cluster specified in ~/.kube/config.
	cd config/manager && $(KUSTOMIZE) edit set image controller=${IMG}
	$(KUSTOMIZE) build config/default | kubectl apply -f -

.PHONY: undeploy
undeploy: ## Undeploy controller from the K8s cluster specified in ~/.kube/config. Call with ignore-not-found=true to ignore resource not found errors during deletion.
	$(KUSTOMIZE) build config/default | kubectl delete --ignore-not-found=$(ignore-not-found) -f -

##@ Build Dependencies

## Location to install dependencies to
LOCALBIN ?= $(shell pwd)/bin
$(LOCALBIN):
	mkdir -p $(LOCALBIN)

## Tool Binaries
KUSTOMIZE ?= $(LOCALBIN)/kustomize
CONTROLLER_GEN ?= $(LOCALBIN)/controller-gen
ENVTEST ?= $(LOCALBIN)/setup-envtest
OPENAPI_GEN ?= $(LOCALBIN)/openapi-gen
DLV ?= $(LOCALBIN)/dlv

## Tool Versions
KUSTOMIZE_VERSION ?= v3.8.7
CONTROLLER_TOOLS_VERSION ?= v0.10.0

KUSTOMIZE_INSTALL_SCRIPT ?= "https://raw.githubusercontent.com/kubernetes-sigs/kustomize/master/hack/install_kustomize.sh"
.PHONY: kustomize
kustomize: $(KUSTOMIZE) ## Download kustomize locally if necessary. If wrong version is installed, it will be removed before downloading.
$(KUSTOMIZE): $(LOCALBIN)
	@if test -x $(LOCALBIN)/kustomize && ! $(LOCALBIN)/kustomize version | grep -q $(KUSTOMIZE_VERSION); then \
		echo "$(LOCALBIN)/kustomize version is not expected $(KUSTOMIZE_VERSION). Removing it before installing."; \
		rm -rf $(LOCALBIN)/kustomize; \
	fi
	test -s $(LOCALBIN)/kustomize || { curl -Ss $(KUSTOMIZE_INSTALL_SCRIPT) | bash -s -- $(subst v,,$(KUSTOMIZE_VERSION)) $(LOCALBIN); }

.PHONY: controller-gen
controller-gen: $(CONTROLLER_GEN) ## Download controller-gen locally if necessary. If wrong version is installed, it will be overwritten.
$(CONTROLLER_GEN): $(LOCALBIN)
	test -s $(LOCALBIN)/controller-gen && $(LOCALBIN)/controller-gen --version | grep -q $(CONTROLLER_TOOLS_VERSION) || \
	GOBIN=$(LOCALBIN) go install sigs.k8s.io/controller-tools/cmd/controller-gen@$(CONTROLLER_TOOLS_VERSION)

.PHONY: envtest
envtest: $(ENVTEST) ## Download envtest-setup locally if necessary.
$(ENVTEST): $(LOCALBIN)
	test -s $(LOCALBIN)/setup-envtest || GOBIN=$(LOCALBIN) go install sigs.k8s.io/controller-runtime/tools/setup-envtest@latest

.PHONY: operator-sdk
OPERATOR_SDK ?= $(LOCALBIN)/operator-sdk
operator-sdk: ## Download operator-sdk locally if necessary.
ifeq (,$(wildcard $(OPERATOR_SDK)))
ifeq (, $(shell which operator-sdk 2>/dev/null))
	@{ \
	set -e ;\
	mkdir -p $(dir $(OPERATOR_SDK)) ;\
	OS=$(shell go env GOOS) && ARCH=$(shell go env GOARCH) && \
	curl -sSLo $(OPERATOR_SDK) https://github.com/operator-framework/operator-sdk/releases/download/$(OPERATOR_SDK_VERSION)/operator-sdk_$${OS}_$${ARCH} ;\
	chmod +x $(OPERATOR_SDK) ;\
	}
else
OPERATOR_SDK = $(shell which operator-sdk)
endif
endif

.PHONY: bundle
bundle: manifests kustomize operator-sdk ## Generate bundle manifests and metadata, then validate generated files.
	$(OPERATOR_SDK) generate kustomize manifests --interactive=false -q
	cd config/manager && $(KUSTOMIZE) edit set image controller=$(IMG) && cd ../..
	$(KUSTOMIZE) build config/manifests | $(OPERATOR_SDK) generate bundle $(BUNDLE_GEN_FLAGS)

.PHONY: bundle-build
bundle-build: ## Build the bundle image.
	docker build -f bundle.Dockerfile -t $(BUNDLE_IMG) .

.PHONY: bundle-push
bundle-push: ## Push the bundle image.
	$(MAKE) docker-push IMG=$(BUNDLE_IMG)

.PHONY: opm
OPM = ./bin/opm
opm: ## Download opm locally if necessary.
ifeq (,$(wildcard $(OPM)))
ifeq (,$(shell which opm 2>/dev/null))
	@{ \
	set -e ;\
	mkdir -p $(dir $(OPM)) ;\
	OS=$(shell go env GOOS) && ARCH=$(shell go env GOARCH) && \
	curl -sSLo $(OPM) https://github.com/operator-framework/operator-registry/releases/download/v1.23.0/$${OS}-$${ARCH}-opm ;\
	chmod +x $(OPM) ;\
	}
else
OPM = $(shell which opm)
endif
endif

# A comma-separated list of bundle images (e.g. make catalog-build BUNDLE_IMGS=example.com/operator-bundle:v0.1.0,example.com/operator-bundle:v0.2.0).
# These images MUST exist in a registry and be pull-able.
BUNDLE_IMGS ?= $(BUNDLE_IMG)

# The image tag given to the resulting catalog image (e.g. make catalog-build CATALOG_IMG=example.com/operator-catalog:v0.2.0).
CATALOG_IMG ?= $(IMAGE_TAG_BASE)-catalog:v$(VERSION)

# Set CATALOG_BASE_IMG to an existing catalog image tag to add $BUNDLE_IMGS to that image.
ifneq ($(origin CATALOG_BASE_IMG), undefined)
FROM_INDEX_OPT := --from-index $(CATALOG_BASE_IMG)
endif

# Build a catalog image by adding bundle images to an empty catalog using the operator package manager tool, 'opm'.
# This recipe invokes 'opm' in 'semver' bundle add mode. For more information on add modes, see:
# https://github.com/operator-framework/community-operators/blob/7f1438c/docs/packaging-operator.md#updating-your-existing-operator
.PHONY: catalog-build
catalog-build: opm ## Build a catalog image.
	$(OPM) index add --container-tool docker --mode semver --tag $(CATALOG_IMG) --bundles $(BUNDLE_IMGS) $(FROM_INDEX_OPT)

# Push the catalog image.
.PHONY: catalog-push
catalog-push: ## Push a catalog image.
	$(MAKE) docker-push IMG=$(CATALOG_IMG)

.PHONY: openapi-gen
openapi-gen: $(OPENAPI_GEN) ## Download envtest-setup locally if necessary.
$(OPENAPI_GEN): $(LOCALBIN)
	test -s $(LOCALBIN)/openapi-gen || GOBIN=$(LOCALBIN) go install k8s.io/kube-openapi/cmd/openapi-gen@649db6989aaecdd64cc4ea16b76e0d0067664001

.PHONY: dlv
dlv: $(DLV) ## Download envtest-setup locally if necessary.
$(DLV): $(LOCALBIN)
	test -s $(LOCALBIN)/dlv || GOBIN=$(LOCALBIN) go install github.com/go-delve/delve/cmd/dlv@latest
