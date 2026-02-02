#!/bin/bash

set -e
# Workaround https://github.com/docker/cli/issues/2533
export DOCKER_API_VERSION=1.43

WORKDIR=$(readlink -f "$(dirname "$0")/../../bin/test_repos")
echo "WORKDIR: ${WORKDIR}"

QUICKSTART_REPO_URL=https://github.com/wildfly/quickstart.git
QUICKSTART_REPO_DIR="${WORKDIR}/wildfly-operator-quickstart"
QUICKSTART_BRANCH=${1:-"38.0.1.Final"}
QUICKSTART=helloworld

BOOTABLE_JAR_REPO_URL=https://github.com/wildfly-extras/wildfly-jar-maven-plugin.git
BOOTABLE_JAR_REPO_DIR="${WORKDIR}/wildfly-jar-maven-plugin"
BOOTABLE_JAR_BRANCH=${2:-"12.0.0.Final"}
BOOTABLE_JAR=examples/microprofile-config

CLUSTER_BENCH_REPO_URL=https://github.com/clusterbench/clusterbench.git
CLUSTER_BENCH_REPO_DIR="${WORKDIR}/clusterbench"
CLUSTER_BENCH_BRANCH=${3:-"12.0.1.Final"}
CLUSTER_BENCH=clusterbench-ee10-ear

echo "Cloning quickstart repo"
rm -rf "${QUICKSTART_REPO_DIR}"
git clone ${QUICKSTART_REPO_URL} "${QUICKSTART_REPO_DIR}"
cd "${QUICKSTART_REPO_DIR}" && git checkout ${QUICKSTART_BRANCH}

echo "Building quickstart image"
cd "${QUICKSTART_REPO_DIR}"/${QUICKSTART} && mvn -U -Popenshift package wildfly:image \
-Dwildfly.image.name=wildfly/wildfly-test-image \
-Dwildfly.image.tag=0.0

echo "Cloning Bootable JAR Maven plugin repo"
rm -rf "${BOOTABLE_JAR_REPO_DIR}"
git clone ${BOOTABLE_JAR_REPO_URL} "${BOOTABLE_JAR_REPO_DIR}"
cd "${BOOTABLE_JAR_REPO_DIR}" && git checkout ${BOOTABLE_JAR_BRANCH}

echo "Building Bootable JAR repo"
cd "${BOOTABLE_JAR_REPO_DIR}" && git checkout ${BOOTABLE_JAR_BRANCH}
cd "${BOOTABLE_JAR_REPO_DIR}"/${BOOTABLE_JAR} && mvn -U -Popenshift package

echo "Building bootable JAR image"
cp "${BOOTABLE_JAR_REPO_DIR}/${BOOTABLE_JAR}/target/microprofile-config-bootable.jar" "${WORKDIR}"
cd "${WORKDIR}"
cat << EOF > "${WORKDIR}/Dockerfile"
FROM mirror.gcr.io/eclipse-temurin:17
EXPOSE 8080 8778 9779
ENV AB_PROMETHEUS_OFF=true AB_JOLOKIA_OFF=true JAVA_OPTIONS=-Djava.net.preferIPv4Stack=true AB_OFF=true
WORKDIR /deployments
COPY microprofile-config-bootable.jar /deployments/microprofile-config-bootable.jar
ENTRYPOINT ["sh", "-c", "java \$JAVA_OPTIONS -jar /deployments/microprofile-config-bootable.jar \$JAVA_ARGS"]
EOF
docker build . -t wildfly/bootable-jar-test-image:0.0

echo "Cloning Clusterbench repo"
rm -rf "${CLUSTER_BENCH_REPO_DIR}"
git clone ${CLUSTER_BENCH_REPO_URL} "${CLUSTER_BENCH_REPO_DIR}"
cd "${CLUSTER_BENCH_REPO_DIR}" && git checkout ${CLUSTER_BENCH_BRANCH}

echo "Building Clusterbench repo"
cd "${CLUSTER_BENCH_REPO_DIR}" && git checkout ${CLUSTER_BENCH_BRANCH}
cd "${CLUSTER_BENCH_REPO_DIR}/${CLUSTER_BENCH}" && mvn -U -Popenshift package wildfly:image \
-Dwildfly.image.name=wildfly/clusterbench-test-image \
-Dwildfly.image.tag=0.0
