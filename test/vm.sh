#!/bin/bash

set -e

DEFAULT_MEMORY="2048"
DEFAULT_CPUS="1"

main() {
  local memory="${MEMORY:-${DEFAULT_MEMORY}}"
  local cpus="${CPUS:-${DEFAULT_CPUS}}"

  local disk="${DISK}"
  if [[ -z "${disk}" ]]; then
    fail "DISK not set"
  fi

  local kernel="${KERNEL}"
  if [[ -z "${kernel}" ]]; then
    fail "KERNEL not set"
  fi

  # read MAC address from /.containerconfig
  local mac="$(jq --raw-output '.MAC' /.containerconfig)"
  if [[ -z "${mac}" ]]; then
    fail "MAC not found in /.containerconfig"
  fi

  # remove the IP and MAC from eth0 and bridge it with a TAP
  # interface so the VM can use the IP and MAC instead
  ip link set eth0 down
  ip addr flush dev eth0
  ip link set eth0 address "$(random_mac)"
  ip link set eth0 up
  ip link add br0 type bridge
  ip link set br0 up
  ip link set dev eth0 master br0
  ip tuntap add dev tap0 mode tap
  ip link set dev tap0 master br0
  ip link set dev tap0 up

  # attach to the console if stdout is a tty
  local append="root=/dev/sda"
  if [[ -t 1 ]]; then
    append="${append} console=ttyS0 console=tty0 noembed nomodeset norestore"
  fi

  # run the VM
  exec /usr/bin/qemu-system-x86_64 \
    -enable-kvm \
    -m      "${memory}" \
    -smp    "${cpus}" \
    -kernel "${kernel}" \
    -append "${append}" \
    -drive  "file=${disk},index=0,media=disk" \
    -device "e1000,netdev=net0,mac=${mac}" \
    -netdev "tap,id=net0,ifname=tap0,script=no,downscript=no" \
    -virtfs "fsdriver=local,path=/opt/flynn-test/backups,security_model=passthrough,readonly,mount_tag=backupsfs" \
    -nographic
}

random_mac () {
  printf 'fe:%02x:%02x:%02x:%02x:%02x' $[RANDOM%256] $[RANDOM%256] $[RANDOM%256] $[RANDOM%256] $[RANDOM%256]
}

fail() {
  local msg=$1
  echo "ERROR: ${msg}" >&2
  exit 1
}

main "$@"
