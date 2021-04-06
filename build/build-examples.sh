#!/usr/bin/env bash

# this is the wildfly-operator repository user name. Useful if you want to trigger the event against your fork
WILDFLY_OPERATOR_REPO_USER="${1:-wildfly}"

if [ -z "${WILDFLY_OPERATOR_GITHUB_PAT}" ];
then
    echo
    echo "Using this script requires the creation of a personal access token here: https://github.com/settings/tokens"
    echo "The token should have the following permissions: repo:public_repo"
    echo "Then export the token in your current shell: export WILDFLY_OPERATOR_GITHUB_PAT=... and re-run this script."
    echo
else
    TARGET_REPO="https://api.github.com/repos/${WILDFLY_OPERATOR_REPO_USER}/wildfly-operator"
    echo "Invoking building examples images workflow on ${TARGET_REPO}"
    curl \
    -X POST \
    -H "Authorization: token ${WILDFLY_OPERATOR_GITHUB_PAT}" \
    -H "Accept: application/vnd.github.ant-man-preview+json" \
    -H "Content-Type: application/json" \
    ${TARGET_REPO}/dispatches \
    -d '{ "event_type" : "build_operator_examples", "client_payload" : { "source" : "wildfly-operator-make" }}'

    result=$?
    if [ "$result" != "0" ]; then
       echo "the curl command failed with: $result"
    else
       echo "Done."
    fi
fi

