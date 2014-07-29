#!/usr/bin/env bash

set -eo pipefail

main() {
  local user=flynn-test
  local dir=/opt/flynn-test
  local bin_dir=$dir/bin
  local build_dir=$dir/build
  local src_dir="$(cd "$(dirname "$0")" && pwd)/.."
  local scripts_dir="$src_dir/scripts"

  apt-get update
  apt-get install -y btrfs-tools zerofree qemu qemu-kvm

  if ! id $user >/dev/null 2>&1; then
    useradd --system --home $dir --user-group --groups kvm -M $user
  fi

  mkdir -p $bin_dir $build_dir

  [ ! -f $bin_dir/flynn ] && install_cli $bin_dir/flynn

  if ! mount | grep -q "tmpfs on $build_dir"; then
    mount_tmpfs $build_dir
  fi

  if [ ! -f $build_dir/rootfs.img ]; then
    $src_dir/rootfs/build.sh $build_dir
  fi

  if ! which godep >/dev/null; then
    install_godep
  fi

  if [ ! -f "$bin_dir/flynn-test" ]; then
    pushd $src_dir >/dev/null
    make
    cp flynn-test flynn-test-runner $bin_dir
    popd >/dev/null
  fi

  rsync -avz "$src_dir/apps" $dir

  chown -R $user:$user $dir

  cp "$scripts_dir/upstart.conf" /etc/init/flynn-test.conf
  [ ! -f "/etc/default/flynn-test" ] && cp "$scripts_dir/defaults.conf" "/etc/default/flynn-test"
  initctl reload-configuration

  echo "Setup finished"
  echo "You should edit /etc/default/flynn-test and then start flynn-test (sudo start flynn-test)"
}

install_cli() {
  local path=$1

  curl -sL -A "`uname -sp`" https://flynn-cli.herokuapp.com/flynn.gz | zcat > $path
  chmod +x $path
}

mount_tmpfs() {
  local dir=$1
  local size=32G

  mount -t tmpfs -o size=$size tmpfs $dir
}

install_godep() {
  mkdir /gopkg
  # use lmars fork until merged: https://github.com/tools/godep/pull/105
  GOPATH=/gopkg go get github.com/lmars/godep
  mv /gopkg/bin/godep /usr/bin
  rm -rf /gopkg
}

main
