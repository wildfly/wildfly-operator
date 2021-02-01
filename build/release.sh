#!/usr/bin/env bash

set -e -x

DRY_RUN=${DRY_RUN:-true}
RELEASE_BRANCH=release_${RELEASE_NAME}

validate() {
  if [ -z "${RELEASE_NAME}" ]; then
     echo "Env variable RELEASE_NAME, which sets version to be released is unset or set to the empty string"
     exit 1
  fi
}

main() {
  validate

  # create release from a branch
  git branch -D ${RELEASE_BRANCH} || true
  git checkout -b ${RELEASE_BRANCH}
  # use the RELEASE_NAME to identify the operator image
  sed -i'.backup' "s/latest/${RELEASE_NAME}/g" deploy/operator.yaml
  git commit -a -m "${RELEASE_NAME} release"
  # tag the release
  git tag ${RELEASE_NAME}
  # put back the "latest" tag in the operator image
  sed -i'.backup' "s/${RELEASE_NAME}/latest/g" deploy/operator.yaml
  git commit -a -m "Prepare for next release"

  if [[ "${DRY_RUN}" = true ]] ; then
    echo "DRY_RUN is set to true. Skipping..."
  else
    # push the tag
    git push upstream ${RELEASE_NAME}
    # merge the release branch into master
    git checkout master
    git merge --ff-only ${RELEASE_BRANCH}
    # and push master to upstream
    git push upstream master
    git branch -D ${RELEASE_BRANCH}
  fi
}

main