#!/bin/bash

set -eo pipefail

go_version="1.13.1"
go_shasum="94f874037b82ea5353f4061e543681a0e79657f787437974214629af8407d124"
protoc_version="3.3.0"
protoc_shasum="feb112bbc11ea4e2f7ef89a359b5e1c04428ba6cfa5ee628c410eccbfe0b64c3"
common_protos_version="1_3_1"
common_protos_shasum="9584b7ac21de5b31832faf827f898671cdcb034bd557a36ea3e7fc07e6571dcb"
gobin_commit="ef6664e41f0bfe3007869844d318bb2bfa2627f9"
dir="/usr/local"

apt-get update
apt-get install --yes git build-essential unzip
apt-get clean

curl -fsSLo /tmp/go.tar.gz "https://storage.googleapis.com/golang/go${go_version}.linux-amd64.tar.gz"
echo "${go_shasum}  /tmp/go.tar.gz" | shasum -c -
rm -rf "${dir}/go"
tar xzf /tmp/go.tar.gz -C "${dir}"
rm /tmp/go.tar.gz

export GOROOT="/usr/local/go"
export GOPATH="/go"
export PATH="${GOROOT}/bin:${PATH}"

# install protobuf compiler
tmpdir=$(mktemp --directory)
trap "rm -rf ${tmpdir}" EXIT
curl -sL https://github.com/google/protobuf/releases/download/v${protoc_version}/protoc-${protoc_version}-linux-x86_64.zip > "${tmpdir}/protoc.zip"
echo "${protoc_shasum}  ${tmpdir}/protoc.zip" | shasum -c -
unzip -d "${tmpdir}/protoc" "${tmpdir}/protoc.zip"
rm -rf /opt/protoc /usr/local/bin/protoc
mv "${tmpdir}/protoc" /opt
ln -s /opt/protoc/bin/protoc /usr/local/bin/protoc

# install googleapis common protos
curl -fSLo "${tmpdir}/common-protos.tar.gz" "https://github.com/googleapis/googleapis/archive/common-protos-${common_protos_version}.tar.gz"
echo "${common_protos_shasum}  ${tmpdir}/common-protos.tar.gz" | shasum -c -
tar xzf "${tmpdir}/common-protos.tar.gz" -C "/opt/protoc/include" --strip-components=1

cp "builder/go-wrapper.sh" "/usr/local/bin/go"
cp "builder/go-wrapper.sh" "/usr/local/bin/cgo"
cp "builder/go-wrapper.sh" "/usr/local/bin/gobin"

# install gobin
git clone https://github.com/flynn/gobin "${tmpdir}/gobin"
cd "${tmpdir}/gobin"
git reset --hard ${gobin_commit}
/usr/local/bin/go build -o /usr/local/bin/gobin-noenv
