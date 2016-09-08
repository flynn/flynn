#!/bin/bash

set -eo pipefail

version=1.7.1
shasum=43ad621c9b014cde8db17393dc108378d37bc853aa351a6c74bf6432c1bbd182
pkg="go${version}.linux-amd64"
go=go/bin/go

test -f ${go} && ${go} version | grep -q ${version} && exit

rm -rf go.tar.gz go

curl -o go.tar.gz -L "https://storage.googleapis.com/golang/${pkg}.tar.gz"
echo "${shasum}  go.tar.gz" | shasum -c -

tar xzf go.tar.gz
rm go.tar.gz
