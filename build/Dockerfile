FROM golang:1.13 AS builder

#This arg is passed by docker buildx & contains the platform info in the form linux/amd64, linux/ppc64le etc.
#Setting the default platform in case buildx is not used.
ARG TARGETPLATFORM="linux/amd64"
#Capture ARCH and write to /goarch file for further reference
RUN [ ! "x" = "x$TARGETPLATFORM" ] && `echo $TARGETPLATFORM |  awk '{split($0,a,"/"); print a[2]}' > /goarch`

WORKDIR /go/src/github.com/wildfly/wildfly-operator
RUN mkdir -p /go/src/github.com/wildfly/wildfly-operator && cd /go/src/github.com/wildfly/wildfly-operator
COPY . .

RUN BIN_DIR=${2-$(pwd)/build/_output/bin} && \
    mkdir -p ${BIN_DIR} && PROJECT_NAME="wildfly-operator" && \
    REPO_PATH="github.com/wildfly/wildfly-operator" && \
    BUILD_PATH="${REPO_PATH}/cmd/manager" && \
    GO_LDFLAGS="-linkmode external -extldflags -static" && \
    echo "building ${PROJECT_NAME}..." && \
    GOOS=linux  GOARCH=$(cat /goarch) CGO_ENABLED=0 && \
    go build -o ${BIN_DIR}/${PROJECT_NAME} -ldflags "${GO_LDFLAGS}" $BUILD_PATH

FROM registry.access.redhat.com/ubi8/ubi-minimal:latest
ENV OPERATOR=/usr/local/bin/wildfly-operator \
    JBOSS_HOME=/wildfly \
    JBOSS_BOOTABLE_HOME=/opt/jboss/container/wildfly-bootable-jar-server \
    JBOSS_BOOTABLE_DATA_DIR=/opt/jboss/container/wildfly-bootable-jar-data \
    USER_UID=1001 \
    USER_NAME=wildfly-operator \
    LABEL_APP_MANAGED_BY=wildfly-operator \
    LABEL_APP_RUNTIME=wildfly
COPY --from=builder /go/src/github.com/wildfly/wildfly-operator/build/_output/bin/wildfly-operator ${OPERATOR}
COPY build/bin /usr/local/bin
RUN  /usr/local/bin/user_setup

ENTRYPOINT ["/usr/local/bin/entrypoint"]

USER ${USER_UID}