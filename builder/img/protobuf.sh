#!/bin/bash

export GOPATH="/go"

gitclone() {
  local repo=$1
  local path="${GOPATH}/src/$2"
  local commit=$3

  if ! [[ -d "${path}" ]]; then
    git clone --no-checkout "https://${repo}" "${path}"
  fi

  pushd "${path}" &>/dev/null
  git checkout "${commit}"
  popd &>/dev/null
}

gitclone \
  "github.com/grpc-ecosystem/grpc-gateway" \
  "github.com/grpc-ecosystem/grpc-gateway" \
  "0c0b378a74040eeb5ef415c5289258a193be0e94"

gitclone \
  "github.com/google/go-genproto" \
  "google.golang.org/genproto/googleapis/api/annotations" \
  "73cb5d0be5af113b42057925bd6c93e3cd9f60fd"
