#!/usr/bin/env bash

set -eo pipefail

info() {
  local msg=$1
  echo "==> $msg"
}

main() {
  local user=flynn-test
  local dir=/opt/flynn-test
  local bin_dir=$dir/bin
  local build_dir=$dir/build
  local test_dir="$(cd "$(dirname "$0")" && pwd)/.."
  local scripts_dir="$test_dir/scripts"

  info "Creating user $user"
  if ! id $user >/dev/null 2>&1; then
    useradd --system --home $dir --user-group --groups kvm -M $user
  fi

  info "Creating directories"
  mkdir -p $bin_dir $build_dir

  info "Mounting build directory"
  if ! mount | grep -q "tmpfs on $build_dir"; then
    mount_tmpfs $build_dir
  fi

  info "Building root filesystem"
  if [ ! -f $build_dir/rootfs.img ]; then
    $test_dir/rootfs/build.sh $build_dir
  fi

  info "Copying apps"
  rsync -avz --quiet "$test_dir/apps" $dir

  info "Fixing permissions"
  chown -R $user:$user $dir

  info "Installing Upstart job"
  cp "$scripts_dir/upstart.conf" /etc/init/flynn-test.conf
  [ ! -f "/etc/default/flynn-test" ] && cp "$scripts_dir/defaults.conf" "/etc/default/flynn-test"
  initctl reload-configuration

  info "Stopping current runner"
  stop flynn-test 2>/dev/null || true

  info "Installing test runner binary"
  cp $test_dir/bin/flynn-test-runner $bin_dir

  info
  info "Setup finished"
  info "You should edit /etc/default/flynn-test and then start flynn-test (sudo start flynn-test)"
}

mount_tmpfs() {
  local dir=$1
  local size=32G

  mount -t tmpfs -o size=$size tmpfs $dir
}

main
