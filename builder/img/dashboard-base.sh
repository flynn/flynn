#!/bin/bash

set -eo pipefail

apt-get update
apt-get install --yes unzip python make
apt-get clean

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

# install yarn
npm install -g yarn@1.21.1
ln -nfs ${nodebin}/yarn /usr/local/bin/yarn
