#!/bin/bash

set -eo pipefail

protoc_version="3.11.0"
protoc_shasum="43dbd9200006152559de2fb9370dbbaac4e711a317a61ba9c1107bb84a27a213"
protoc_url="https://github.com/google/protobuf/releases/download/v${protoc_version}/protoc-${protoc_version}-linux-x86_64.zip"

apt-get update
apt-get install --yes unzip
apt-get clean

# install protobuf compiler
curl -sL "${protoc_url}" > /tmp/protoc.zip
echo "${protoc_shasum}  /tmp/protoc.zip" | shasum -c -
unzip -d /usr/local /tmp/protoc.zip
rm /tmp/protoc.zip
