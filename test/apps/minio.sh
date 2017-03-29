#!/bin/bash

set -e

URL="https://dl.minio.io/server/minio/release/linux-amd64/archive/minio.RELEASE.2017-03-16T21-50-32Z"
SHA="c7952571e1640dbd3106dd05a977dbd17b82028c47401ed07b8c2278c2130967"

if [[ -e "bin/minio" ]]; then
  exit
fi

TMP="$(mktemp --directory)"
trap "rm -rf ${TMP}" EXIT

curl -fsSLo "${TMP}/minio" "${URL}"
echo "${SHA}  ${TMP}/minio" | shasum -a 256 -c

mkdir -p "bin"
mv "${TMP}/minio" "bin/minio"
chmod +x "bin/minio"
