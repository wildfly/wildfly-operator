set -o errexit
set -o nounset

if ! [ -x "$(command -v operator-sdk)" ]; then
  echo 'Install operator-sdk' >&2
  CWD="$(pwd)"
  mkdir -p $GOPATH/src/github.com/operator-framework
  cd $GOPATH/src/github.com/operator-framework
  git clone https://github.com/operator-framework/operator-sdk
  cd operator-sdk
  git checkout master
  make dep
  make install
  cd $CWD
fi

operator-sdk version