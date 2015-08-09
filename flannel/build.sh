#!/bin/bash

set -eo pipefail

commit=1d748d62859b74aa1fb441c14393845704f85c8b
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
