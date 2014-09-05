#!/bin/bash
set -xeo pipefail

GOPATH=~/.go
FLYNN=$GOPATH/src/github.com/flynn/flynn

git clone https://github.com/flynn/flynn.git $FLYNN

cat <<EOF >> ~/.bashrc
export GOPATH=$GOPATH
export PATH=\$GOPATH/bin:\$PATH
cd $FLYNN
EOF
