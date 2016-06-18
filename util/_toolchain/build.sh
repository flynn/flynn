#!/bin/bash

set -eo pipefail

version=1.6.2
shasum=e40c36ae71756198478624ed1bb4ce17597b3c19d243f3f0899bb5740d56212a
pkg="go${version}.linux-amd64"
go=go/bin/go

test -f ${go} && ${go} version | grep -q ${version} && exit

rm -rf go.tar.gz go

curl -o go.tar.gz -L "https://storage.googleapis.com/golang/${pkg}.tar.gz"
echo "${shasum}  go.tar.gz" | shasum -c -

tar xzf go.tar.gz
rm go.tar.gz
