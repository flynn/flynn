#!/bin/bash

set -eo pipefail

commit=6edbd51ce0385fbcdbe9064ecbe55a48007eb3e2
dir=flannel-${commit}
tmpdir=$(mktemp --directory)

cleanup() {
  rm -rf "${tmpdir}"
}
trap cleanup EXIT

mkdir -p bin
pushd "${tmpdir}" >/dev/null
curl -L "https://github.com/flynn/flannel/archive/${commit}.tar.gz" | tar xz
cd "${dir}"
./build
popd >/dev/null

cp "${tmpdir}/${dir}/bin/flanneld" bin/
