#!/bin/bash
set -e -x

truncate -s 16G rootfs.img
mkfs.ext4 -Fq rootfs.img

dir=$(mktemp -d)
sudo mount -o loop rootfs.img $dir

function cleanup {
  sudo umount $dir
  rm -rf $dir
}
trap cleanup ERR

curl -L http://cdimage.ubuntu.com/ubuntu-core/releases/14.04/release/ubuntu-core-14.04-core-amd64.tar.gz | sudo tar -xzC $dir

sudo chroot $dir bash < setup.sh

cleanup

zerofree rootfs.img
tar -Sc rootfs.img | pxz -9e - > rootfs.img.tar.xz
