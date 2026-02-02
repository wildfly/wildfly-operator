#!/usr/bin/env bash

set -o errexit
set -o nounset
set -o pipefail

IMAGE_NAME=${1:-}
PROJECT_NAME="wildfly-operator"
REPO_PATH="github.com/wildfly/wildfly-operator"
VERSION="$(git describe --tags --always --dirty)"
GO_LDFLAGS_ARG="-X '${REPO_PATH}/version.Version=${VERSION}'"
echo "building ${PROJECT_NAME} and tagging with ${IMAGE_NAME}"

# Workaround https://github.com/docker/cli/issues/2533
export DOCKER_API_VERSION=1.43

docker build -t ${IMAGE_NAME} --build-arg GO_LDFLAGS="${GO_LDFLAGS_ARG}" .
