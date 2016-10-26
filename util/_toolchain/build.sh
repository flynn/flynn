#!/bin/bash

set -eo pipefail

version=1.7.3
shasum=508028aac0654e993564b6e2014bf2d4a9751e3b286661b0b0040046cf18028e
pkg="go${version}.linux-amd64"
go=go/bin/go

test -f ${go} && ${go} version | grep -q ${version} && exit

rm -rf go.tar.gz go

curl -o go.tar.gz -L "https://storage.googleapis.com/golang/${pkg}.tar.gz"
echo "${shasum}  go.tar.gz" | shasum -c -

tar xzf go.tar.gz
rm go.tar.gz
