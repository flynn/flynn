#!/bin/bash

set -eo pipefail

version=1.7.5
shasum=2e4dd6c44f0693bef4e7b46cc701513d74c3cc44f2419bf519d7868b12931ac3
pkg="go${version}.linux-amd64"
go=go/bin/go

test -f ${go} && ${go} version | grep -q ${version} && exit

rm -rf go.tar.gz go

curl -o go.tar.gz -L "https://storage.googleapis.com/golang/${pkg}.tar.gz"
echo "${shasum}  go.tar.gz" | shasum -c -

tar xzf go.tar.gz
rm go.tar.gz
