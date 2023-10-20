# Build the manager binary
FROM golang:1.21.3 as builder

WORKDIR /workspace
# Copy the Go Modules manifests
COPY go.mod go.mod
COPY go.sum go.sum
# cache deps before building and copying source so that we don't need to re-download as much
# and so that source changes don't invalidate our downloaded layer
RUN go mod download

# Copy the go source
COPY main.go main.go
COPY api/ api/
COPY controllers/ controllers/
COPY pkg/ pkg/
COPY version/ version/

# Build
ARG GO_LDFLAGS=""
RUN echo "FLAGS: $GO_LDFLAGS"
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 GO111MODULE=on go build -a -o manager -ldflags="${GO_LDFLAGS}" main.go

FROM registry.access.redhat.com/ubi8/ubi-minimal:latest
ENV OPERATOR=/usr/local/bin/wildfly-operator \
    JBOSS_HOME=/wildfly \
    JBOSS_BOOTABLE_HOME=/opt/jboss/container/wildfly-bootable-jar-server \
    JBOSS_BOOTABLE_DATA_DIR=/opt/jboss/container/wildfly-bootable-jar-data \
    USER_UID=1001 \
    USER_NAME=wildfly-operator \
    LABEL_APP_MANAGED_BY=wildfly-operator \
    LABEL_APP_RUNTIME=wildfly

WORKDIR /
COPY --from=builder /workspace/manager ${OPERATOR}

COPY build/bin /usr/local/bin
RUN  /usr/local/bin/user_setup

USER ${USER_UID}

ENTRYPOINT ["/usr/local/bin/entrypoint"]
