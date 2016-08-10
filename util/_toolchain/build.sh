#!/bin/bash

set -eo pipefail

version=1.7rc6
shasum=45e3dfba542927ea58146a5d47a983feb36401ccafeea28a9e0a79534738b154
pkg="go${version}.linux-amd64"
go=go/bin/go

test -f ${go} && ${go} version | grep -q ${version} && exit

rm -rf go.tar.gz go

curl -o go.tar.gz -L "https://storage.googleapis.com/golang/${pkg}.tar.gz"
echo "${shasum}  go.tar.gz" | shasum -c -

tar xzf go.tar.gz
rm go.tar.gz
