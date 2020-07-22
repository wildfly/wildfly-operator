set -o errexit
set -o nounset

if ! [ -e "./operator-sdk" ]; then
  echo 'Install operator-sdk' >&2
  RELEASE_VERSION=v0.17.2
  case "$(uname)" in
    Darwin*)    file=operator-sdk-${RELEASE_VERSION}-x86_64-apple-darwin;;
    *)          file=operator-sdk-${RELEASE_VERSION}-x86_64-linux-gnu;;
  esac

  ARCH=$(uname -m) 
  if [[ ${ARCH} == 'ppc64le' ]]; then file=operator-sdk-${RELEASE_VERSION}-ppc64le-linux-gnu; fi

  curl -LO https://github.com/operator-framework/operator-sdk/releases/download/${RELEASE_VERSION}/${file}
  chmod +x ${file} && mv ${file} ./operator-sdk
fi

./operator-sdk version
