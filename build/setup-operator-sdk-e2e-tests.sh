set -o errexit
set -o nounset

if ! [ -e "./operator-sdk-e2e-tests" ]; then
  echo 'Install operator-sdk-e2e-tests' >&2
  RELEASE_VERSION=v0.18.2
  case "$(uname)" in
    Darwin*)    file=operator-sdk-${RELEASE_VERSION}-x86_64-apple-darwin;;
    *)          file=operator-sdk-${RELEASE_VERSION}-x86_64-linux-gnu;;
  esac

  curl -LO https://github.com/operator-framework/operator-sdk/releases/download/${RELEASE_VERSION}/${file}
  chmod +x ${file} && mv ${file} ./operator-sdk-e2e-tests
fi

./operator-sdk-e2e-tests version
