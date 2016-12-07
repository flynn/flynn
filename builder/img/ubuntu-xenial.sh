#!/bin/bash

TMP="$(mktemp --directory)"

URL="https://partner-images.canonical.com/core/xenial/20161213/ubuntu-xenial-core-cloudimg-amd64-root.tar.gz"
SHA="2dd71032b37fbe1f14b3db10fd2737ac2f533d69f09232516e335fc3b4e291ed"
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
