#!/bin/bash
#
# A script to start flynn-host inside a container.

# exit on error
set -e

DEFAULT_KVM_MEMORY="2048"
DEFAULT_KVM_CPUS="1"

main() {
  # the --systemd flag indicates that the script was executed by systemd
  # inside a VM, in which case start flynn-host in the VM
  if [[ $1 = '--systemd' ]]; then
    start_flynn_host_in_vm
  elif [[ -e /dev/kvm ]]; then
    start_kvm
  else
    start_flynn_host_in_container
  fi
}

start_flynn_host_in_vm() {
  # load the environment variables
  source <(jq -r '.Env | to_entries[] | "export \(.key)=\(.value)"' /.containerconfig)

  # mount /dev/sda as an ext4 filesystem at /var/lib/flynn and use it as the
  # temp disk to avoid EINVAL issues with 9p filesystems
  mkfs.ext4 -L FLYNN /dev/sda
  mkdir -p /var/lib/flynn
  mount /dev/sda /var/lib/flynn
  mkdir -p /var/lib/flynn/tmp
  export TMPDIR=/var/lib/flynn/tmp

  # mount the layer cache so that built images can be mounted
  mkdir -p /var/lib/flynn/layer-cache
  mount -t 9p -o trans=virtio layer-cache /var/lib/flynn/layer-cache

  # setup the daemon args
  local args=(--no-resurrect)
  if [[ -n "${DISCOVERY_SERVICE}" ]]; then
    args+=(--discovery-service "${DISCOVERY_SERVICE}")
  fi

  # start flynn-host
  exec /usr/local/bin/flynn-host daemon ${args[@]}
}

start_kvm() {
  local memory="${MEMORY:-${DEFAULT_KVM_MEMORY}}"
  local cpus="${CPUS:-${DEFAULT_KVM_CPUS}}"

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

  # use the container's filesystem as the root VM filesystem
  local cmdline="root=rootfs rw rootfstype=9p rootflags=trans=virtio"

  # set /etc/hostname
  local hostname="$(jq --raw-output '.Hostname' /.containerconfig)"
  if [[ -z "${hostname}" ]]; then
    fail "Hostname not found in /.containerconfig"
  fi
  echo "${hostname}" > /etc/hostname

  # attach to the console if stdout is a tty
  if [[ -t 1 ]]; then
    cmdline="${cmdline} console=ttyS0 console=tty0 noembed nomodeset norestore"
  fi

  # create a dedicated disk for /var/lib/flynn
  truncate -s 10G /tmp/flynn.disk

  # run the VM
  exec /usr/bin/qemu-system-x86_64 \
    -enable-kvm \
    -m      "${memory}" \
    -smp    "${cpus}" \
    -kernel /boot/vmlinuz-* \
    -initrd /boot/initrd.img-* \
    -append "${cmdline}" \
    -virtfs "fsdriver=local,path=/,security_model=passthrough,mount_tag=rootfs" \
    -drive  "file=/tmp/flynn.disk,format=raw,index=0,media=disk" \
    -virtfs "fsdriver=local,path=/var/lib/flynn/layer-cache,security_model=passthrough,mount_tag=layer-cache" \
    -device "virtio-net,netdev=net0,mac=${mac}" \
    -netdev "tap,id=net0,ifname=tap0,script=no,downscript=no" \
    -nographic
}

start_flynn_host_in_container() {
  # create /etc/mtab to keep ZFS happy
  ln -nfs /proc/mounts /etc/mtab

  # start udevd so that ZFS device nodes and symlinks are created in our mount
  # namespace
  /lib/systemd/systemd-udevd --daemon

  # use a unique directory in /var/lib/flynn (which is bind mounted from the
  # host)
  DIR="/var/lib/flynn/${FLYNN_JOB_ID}"
  mkdir -p "${DIR}"

  # create a tmpdir in /var/lib/flynn to avoid ENOSPC when downloading image
  # layers
  export TMPDIR="${DIR}/tmp"
  mkdir -p "${TMPDIR}"

  # use a unique zpool to avoid conflicts with other daemons
  ZPOOL="flynn-${FLYNN_JOB_ID}"

  ARGS=(
    --state      "${DIR}/host-state.bolt"
    --sink-state "${DIR}/sink-state.bolt"
    --volpath    "${DIR}/volumes"
    --log-dir    "${DIR}/logs"
    --log-file   "/dev/stdout"
    --zpool-name "${ZPOOL}"
    --no-resurrect
  )

  if [[ -n "${DISCOVERY_SERVICE}" ]]; then
    ARGS+=(
      --discovery-service "${DISCOVERY_SERVICE}"
    )
  fi

  # start flynn-host
  exec /usr/local/bin/flynn-host daemon ${ARGS[@]}
}

random_mac () {
  printf 'fe:%02x:%02x:%02x:%02x:%02x' $[RANDOM%256] $[RANDOM%256] $[RANDOM%256] $[RANDOM%256] $[RANDOM%256]
}

fail() {
  local msg=$1
  echo "ERROR: ${msg}" >&2
  exit 1
}

main $@
