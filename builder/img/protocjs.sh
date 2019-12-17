#!/bin/bash

set -eo pipefail

# install googleapis common protos
shasum="9584b7ac21de5b31832faf827f898671cdcb034bd557a36ea3e7fc07e6571dcb"
curl -fSLo /tmp/common-protos.tar.gz "https://github.com/googleapis/googleapis/archive/common-protos-1_3_1.tar.gz"
echo "${shasum}  /tmp/common-protos.tar.gz" | shasum -c -
tar xzf /tmp/common-protos.tar.gz -C "/usr/local/include" --strip-components=1
rm /tmp/common-protos.tar.gz

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
