#!/bin/bash
#
# go-wrapper.sh is placed in /usr/local/bin/go and /usr/local/bin/cgo
# inside the Go image and sets some environment variables and flags
# before running the go tool.

set -eo pipefail

export GOROOT="/usr/local/go"
export GOPATH="/go"
export GO111MODULE=on

if [[ -z "$GOFLAGS" ]]; then
  # make sure we don't overwrite flags for `go list` run by gobin
  export GOFLAGS=-mod=vendor
fi

if [[ $(basename $0) != "cgo" ]]; then
  export CGO_ENABLED=0
fi

BIN="${GOROOT}/bin/go"
if [[ $(basename $0) == "gobin" ]]; then
  export GOPROXY=https://proxy.golang.org
  export GOFLAGS=-mod=readonly
  BIN=/usr/local/bin/gobin-noenv
fi

GO_LDFLAGS="-X github.com/flynn/flynn/pkg/version.version=${FLYNN_VERSION}"

if [[ -n "${TUF_ROOT_KEYS}" ]]; then
  GO_LDFLAGS="${GO_LDFLAGS} -X github.com/flynn/flynn/pkg/tufconfig.RootKeysJSON=${TUF_ROOT_KEYS}"
fi

if [[ -n "${TUF_REPOSITORY}" ]]; then
  GO_LDFLAGS="${GO_LDFLAGS} -X github.com/flynn/flynn/pkg/tufconfig.Repository=${TUF_REPOSITORY}"
fi

if [[ "$1" = "build" ]]; then
	${BIN} $1 -ldflags "${GO_LDFLAGS}" ${@:2}
else
	${BIN} "$@"
fi
