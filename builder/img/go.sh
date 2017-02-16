#!/bin/bash

set -eo pipefail

version="1.8.3"
shasum="1862f4c3d3907e59b04a757cfda0ea7aa9ef39274af99a784f5be843c80c6772"
dir="/usr/local"

apt-get update
apt-get install --yes git build-essential unzip
apt-get clean

curl -fsSLo /tmp/go.tar.gz "https://storage.googleapis.com/golang/go${version}.linux-amd64.tar.gz"
echo "${shasum}  /tmp/go.tar.gz" | shasum -c -
tar xzf /tmp/go.tar.gz -C "${dir}"
rm /tmp/go.tar.gz

export GOROOT="/usr/local/go"
export GOPATH="/go"
export PATH="${GOROOT}/bin:${PATH}"

# install protobuf compiler
tmpdir=$(mktemp --directory)
trap "rm -rf ${tmpdir}" EXIT
curl -sL https://github.com/google/protobuf/releases/download/v3.3.0/protoc-3.3.0-linux-x86_64.zip > "${tmpdir}/protoc.zip"
unzip -d "${tmpdir}/protoc" "${tmpdir}/protoc.zip"
mv "${tmpdir}/protoc" /opt
ln -s /opt/protoc/bin/protoc /usr/local/bin/protoc

goinstall() {
  local repo=$1
  local pkg=$2
  local commit=$3
  local out=$4

  local path="${GOPATH}/src/${repo}"
  if ! [[ -d "${path}" ]]; then
    git clone --no-checkout "https://${repo}" "${path}"
  fi

  pushd "${path}" &>/dev/null
  git checkout "${commit}"
  go build -o "${out}" "${pkg}"
  popd &>/dev/null

  rm -rf "${path}"
}

goinstall \
  "github.com/jteeuwen/go-bindata" \
  "./go-bindata" \
  "a0ff2567cfb70903282db057e799fd826784d41d" \
  "/bin/go-bindata"

goinstall \
  "github.com/tools/godep" \
  "." \
  "796a3227145680d8be9aede03e98d9ee9c9c93fc" \
  "/bin/godep"

mkdir -p "${GOPATH}/src/github.com/flynn"
ln -nfs "$(pwd)" "${GOPATH}/src/github.com/flynn/flynn"

cp "builder/go-wrapper.sh" "/usr/local/bin/go"
cp "builder/go-wrapper.sh" "/usr/local/bin/cgo"
