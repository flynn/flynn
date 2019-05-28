#!/bin/bash

TMP="$(mktemp --directory)"

URL="https://partner-images.canonical.com/core/bionic/20190621/ubuntu-bionic-core-cloudimg-amd64-root.tar.gz"
SHA="ed1753585d70724010e9ca26cf47337201ecc5c65c7251ca7a97b5d1c0ed6365"
curl -fSLo "${TMP}/ubuntu.tar.gz" "${URL}"
echo "${SHA}  ${TMP}/ubuntu.tar.gz" | sha256sum -c -

mkdir -p "${TMP}/root"
tar xf "${TMP}/ubuntu.tar.gz" -C "${TMP}/root"

cp "/etc/resolv.conf" "${TMP}/root/etc/resolv.conf"
cleanup() {
  >"${TMP}/root/etc/resolv.conf"
}
trap cleanup EXIT

chroot "${TMP}/root" bash -e < "builder/ubuntu-setup.sh"

mksquashfs "${TMP}/root" "/mnt/out/layer.squashfs" -noappend
