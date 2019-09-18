#!/bin/bash

set -eo pipefail

if [ ! -f build/bin/flynn-builder ]; then
  echo "Bootstrapping Go/flynn-builder..."
  builder/img/go.sh
  mkdir -p build/bin
  go build -o build/bin/flynn-builder ./builder
fi

export GO111MODULE=on
export GOFLAGS=-mod=vendor

build/bin/flynn-builder "$@"
