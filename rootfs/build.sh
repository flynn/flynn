#!/bin/bash
set -e -x

src_dir="$(cd "$(dirname "$0")" && pwd)"
build_dir=${1:-.}

truncate -s 16G $build_dir/rootfs.img
mkfs.ext4 -FqL rootfs $build_dir/rootfs.img

dir=$(mktemp -d)
sudo mount -o loop $build_dir/rootfs.img $dir

function cleanup {
  sudo umount $dir
  rm -rf $dir
}
trap cleanup ERR

curl -L http://cdimage.ubuntu.com/ubuntu-core/releases/14.04/release/ubuntu-core-14.04-core-amd64.tar.gz | sudo tar -xzC $dir

sudo chroot $dir bash < "$src_dir/setup.sh"

sudo cp $dir/boot/vmlinuz-* $build_dir/vmlinuz

cleanup

zerofree $build_dir/rootfs.img
