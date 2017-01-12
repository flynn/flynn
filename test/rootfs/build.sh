#!/bin/bash
set -e -x

src_dir="$(cd "$(dirname "$0")" && pwd)"
build_dir=${1:-.}

truncate -s 70G ${build_dir}/rootfs.img
mkfs.ext4 -FqL rootfs ${build_dir}/rootfs.img

dir=$(mktemp -d)
sudo mount -o loop ${build_dir}/rootfs.img ${dir}

cleanup() {
  sudo umount ${dir}
  rm -rf ${dir}
}
trap cleanup ERR

image="http://cdimage.ubuntu.com/ubuntu-base/releases/14.04/release/ubuntu-base-14.04-core-amd64.tar.gz"
curl -L ${image} | sudo tar -xzC ${dir}

# use jchroot (https://github.com/vincentbernat/jchroot) which uses a PID
# namespace so daemons do not outlive the setup and prevent unmounting rootfs.img
sudo jchroot ${dir} bash < "${src_dir}/setup.sh"

sudo cp ${dir}/boot/vmlinuz-* ${build_dir}/vmlinuz

cleanup

zerofree ${build_dir}/rootfs.img
