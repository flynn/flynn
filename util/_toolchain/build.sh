#!/bin/bash

set -eo pipefail

version=1.7rc1
shasum=afe956b6d323c68fbd851f4e962f26f16dde61d7caa1de1a8408c7de0b6034aa
pkg="go${version}.linux-amd64"
go=go/bin/go

test -f ${go} && ${go} version | grep -q ${version} && exit

rm -rf go.tar.gz go

curl -o go.tar.gz -L "https://storage.googleapis.com/golang/${pkg}.tar.gz"
echo "${shasum}  go.tar.gz" | shasum -c -

tar xzf go.tar.gz
rm go.tar.gz
