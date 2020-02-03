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

# install nodejs
nodeversion="8.11.4"
nodeshasum="c69abe770f002a7415bd00f7ea13b086650c1dd925ef0c3bf8de90eabecc8790"
curl -fSLo /tmp/node.tar.gz "https://nodejs.org/dist/v${nodeversion}/node-v${nodeversion}-linux-x64.tar.gz"
echo "${nodeshasum}  /tmp/node.tar.gz" | shasum -c -
tar xzf /tmp/node.tar.gz -C "/usr/local"
rm /tmp/node.tar.gz
# link nodejs binary
nodebin="/usr/local/node-v${nodeversion}-linux-x64/bin"
ln -nfs ${nodebin}/node /usr/local/bin/node
ln -nfs ${nodebin}/npm /usr/local/bin/npm

# install typescript protoc (https://github.com/improbable-eng/ts-protoc-gen)
npm install -g google-protobuf@3.11.2 ts-protoc-gen@0.12.0
ln -nfs ${nodebin}/protoc-gen-ts /usr/local/bin/protoc-gen-ts
