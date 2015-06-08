#!/bin/bash

set -eo pipefail

commit=829976fc974e6f0e7f0d31db2451951728627569
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
