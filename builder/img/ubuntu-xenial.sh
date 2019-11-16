#!/bin/bash

TMP="$(mktemp --directory)"

URL="https://partner-images.canonical.com/core/xenial/20191108/ubuntu-xenial-core-cloudimg-amd64-root.tar.gz"
SHA="bd9f1a6f5da8379ab0918ea4b36ccf93d35b0052f2e47bad659794d0cd91aa5f"
curl -fSLo "${TMP}/ubuntu.tar.gz" "${URL}"
echo "${SHA}  ${TMP}/ubuntu.tar.gz" | sha256sum -c -

mkdir -p "${TMP}/root"
tar xf "${TMP}/ubuntu.tar.gz" -C "${TMP}/root"

cp "/etc/resolv.conf" "${TMP}/root/etc/resolv.conf"
mount --bind "/dev/pts" "${TMP}/root/dev/pts"
cleanup() {
  umount "${TMP}/root/dev/pts"
  >"${TMP}/root/etc/resolv.conf"
}
trap cleanup EXIT

chroot "${TMP}/root" bash -e < "builder/ubuntu-setup.sh"

mksquashfs "${TMP}/root" "/mnt/out/layer.squashfs" -noappend
