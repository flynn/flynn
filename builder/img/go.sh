#!/bin/bash

set -eo pipefail

go_version="1.13.1"
go_shasum="94f874037b82ea5353f4061e543681a0e79657f787437974214629af8407d124"
gobin_commit="ef6664e41f0bfe3007869844d318bb2bfa2627f9"
dir="/usr/local"

apt-get update
apt-get install --yes git build-essential
apt-get clean

curl -fsSLo /tmp/go.tar.gz "https://storage.googleapis.com/golang/go${go_version}.linux-amd64.tar.gz"
echo "${go_shasum}  /tmp/go.tar.gz" | shasum -c -
rm -rf "${dir}/go"
tar xzf /tmp/go.tar.gz -C "${dir}"
rm /tmp/go.tar.gz

export GOROOT="/usr/local/go"
export GOPATH="/go"
export PATH="${GOROOT}/bin:${PATH}"

cp "builder/go-wrapper.sh" "/usr/local/bin/go"
cp "builder/go-wrapper.sh" "/usr/local/bin/cgo"
cp "builder/go-wrapper.sh" "/usr/local/bin/gobin"

# install gobin
git clone https://github.com/flynn/gobin "/tmp/gobin"
trap "rm -rf /tmp/gobin" EXIT
cd "/tmp/gobin"
git reset --hard ${gobin_commit}
/usr/local/bin/go build -o /usr/local/bin/gobin-noenv
