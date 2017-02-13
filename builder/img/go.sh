#!/bin/bash

set -eo pipefail

version="1.7.4"
shasum="47fda42e46b4c3ec93fa5d4d4cc6a748aa3f9411a2a2b7e08e3a6d80d753ec8b"
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
protoc_sha="0efa8b7ca6913cf8fb0dfc22d98b0a04eb194d7f"
curl -fsSLo /tmp/protoc.zip https://github.com/google/protobuf/releases/download/v3.3.0/protoc-3.3.0-linux-x86_64.zip
echo "${protoc_sha}  /tmp/protoc.zip" | shasum -c -
unzip -d /opt/protoc /tmp/protoc.zip
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

goinstall \
  "github.com/golang/protobuf" \
  "./protoc-gen-go" \
  "6a1fa9404c0aebf36c879bc50152edcc953910d2" \
  "/bin/protoc-gen-go"

mkdir -p "${GOPATH}/src/github.com/flynn"
ln -nfs "$(pwd)" "${GOPATH}/src/github.com/flynn/flynn"

cp "builder/go-wrapper.sh" "/usr/local/bin/go"
cp "builder/go-wrapper.sh" "/usr/local/bin/cgo"
