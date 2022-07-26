set -o errexit
set -o nounset

if ! [ -e "./operator-sdk" ]; then
  echo 'Install operator-sdk' >&2
  RELEASE_VERSION=v1.3.2
  case "$(uname)" in
    Darwin*)    file=operator-sdk_darwin_amd64;;
    *)          file=helm-operator_linux_amd64;;
  esac

  ARCH=$(uname -m)
  if [[ ${ARCH} == 'ppc64le' ]]; then
    file=operator-sdk_linux_ppc64le;
  fi

  curl -LO https://github.com/operator-framework/operator-sdk/releases/download/${RELEASE_VERSION}/${file}
  chmod +x ${file} && mv ${file} ./operator-sdk
fi

./operator-sdk version
