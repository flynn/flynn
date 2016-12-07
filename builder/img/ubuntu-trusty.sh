#!/bin/bash

TMP="$(mktemp --directory)"

URL="https://partner-images.canonical.com/core/trusty/20161215/ubuntu-trusty-core-cloudimg-amd64-root.tar.gz"
SHA="3c5c465ca5c2da880e58c5e11ebf27c5e0df3c9de8e279091a86fe30f7cd8495"
curl -fSLo "${TMP}/ubuntu.tar.gz" "${URL}"
echo "${SHA}  ${TMP}/ubuntu.tar.gz" | sha256sum -c -

mkdir -p "${TMP}/root"
tar xf "${TMP}/ubuntu.tar.gz" -C "${TMP}/root"

cp "/etc/resolv.conf" "${TMP}/root/etc/resolv.conf"
chroot "${TMP}/root" bash -e < "builder/ubuntu-setup.sh"

>"${TMP}/root/etc/resolv.conf"
mksquashfs "${TMP}/root" "/mnt/out/layer.squashfs" -noappend
