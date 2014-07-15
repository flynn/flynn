#!/bin/bash
set -e -x

truncate -s 16G rootfs.img
mkfs.ext4 -FqL rootfs rootfs.img

dir=$(mktemp -d)
sudo mount -o loop rootfs.img $dir

function cleanup {
  sudo umount $dir
  rm -rf $dir
}
trap cleanup ERR

curl -L http://cdimage.ubuntu.com/ubuntu-core/releases/14.04/release/ubuntu-core-14.04-core-amd64.tar.gz | sudo tar -xzC $dir

sudo chroot $dir bash < setup.sh

sudo cp $dir/boot/vmlinuz-* vmlinuz

cleanup

zerofree rootfs.img
