#!/bin/bash

set -eo pipefail

commit=e096282d63d1ca8c3d9b6e689da6a7d2b8ea0740
dir=flannel-${commit}
tmpdir=$(mktemp --directory)
PATH=$(readlink -f ../util/_toolchain/go/bin):$PATH

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
