#!/bin/bash

TMP="$(mktemp --directory)"

URL="https://partner-images.canonical.com/core/trusty/20190502/ubuntu-trusty-core-cloudimg-amd64-root.tar.gz"
SHA="2b1d09d5ba303e924bb8abf402620a6519bc5b8ef7b52c9179c5e958bd2e4e3f"
curl -fSLo "${TMP}/ubuntu.tar.gz" "${URL}"
echo "${SHA}  ${TMP}/ubuntu.tar.gz" | sha256sum -c -

mkdir -p "${TMP}/root"
tar xf "${TMP}/ubuntu.tar.gz" -C "${TMP}/root"

cp "/etc/resolv.conf" "${TMP}/root/etc/resolv.conf"
chroot "${TMP}/root" bash -e < "builder/ubuntu-setup.sh"

>"${TMP}/root/etc/resolv.conf"
mksquashfs "${TMP}/root" "/mnt/out/layer.squashfs" -noappend
