#!/bin/bash

set -eo pipefail

version="1.12.8"
shasum="bd26cd4962a362ed3c11835bca32c2e131c2ae050304f2c4df9fa6ded8db85d2"
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

mkdir -p "${GOPATH}/src/github.com/flynn"
ln -nfs "$(pwd)" "${GOPATH}/src/github.com/flynn/flynn"

cp "builder/go-wrapper.sh" "/usr/local/bin/go"
cp "builder/go-wrapper.sh" "/usr/local/bin/cgo"
