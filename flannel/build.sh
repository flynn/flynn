#!/bin/bash

set -eo pipefail

commit=04ff05d566d705715882b20a566be6113ca50679
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
