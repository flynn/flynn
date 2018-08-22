#!/bin/bash
set -e -x

src_dir="$(cd "$(dirname "$0")" && pwd)"
build_dir=${1:-.}

truncate -s 70G ${build_dir}/rootfs.img
mkfs.ext4 -FqL rootfs ${build_dir}/rootfs.img

dir=$(mktemp -d)
mount -o loop ${build_dir}/rootfs.img ${dir}

image="http://cdimage.ubuntu.com/ubuntu-base/releases/16.04/release/ubuntu-base-16.04.4-base-amd64.tar.gz"
curl -L ${image} | tar -xzC ${dir}

mount -t proc proc "${dir}/proc"
chroot ${dir} bash < "${src_dir}/setup.sh"

cp ${dir}/boot/vmlinuz-* ${build_dir}/vmlinuz

umount "${dir}/proc"
umount "${dir}"
zerofree ${build_dir}/rootfs.img
