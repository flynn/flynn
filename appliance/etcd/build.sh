#!/bin/bash

set -eo pipefail

version=2.0.0-rc.1
tmpdir=$(mktemp --directory)
pkg="etcd-v${version}-linux-amd64"

cleanup() {
  rm -rf "${tmpdir}"
}
trap cleanup EXIT

curl -L "https://github.com/coreos/etcd/releases/download/v${version}/${pkg}.tar.gz" | tar xzC "${tmpdir}"
mv ${tmpdir}/${pkg}/{etcd,etcdctl} .
