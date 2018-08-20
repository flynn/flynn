#!/bin/bash

set -e

main() {
  local dir="/opt/flynn-test"
  if ! [[ -d "${dir}" ]]; then
    fail "missing /opt/flynn-test directory"
  fi

  mkdir -p "${dir}/build" "${dir}/backups"
  if ! test -f "${dir}/build/rootfs.img" || ! test -f "${dir}/build/vmlinuz"; then
    /test/rootfs/build.sh "${dir}/build"
  fi

  export TMPDIR="${dir}/build"

  cd "${dir}"
  exec /bin/flynn-test-runner \
    --rootfs   "${dir}/build/rootfs.img" \
    --kernel   "${dir}/build/vmlinuz" \
    --assets   "/test/assets" \
    --backups-dir "${dir}/backups" \
    --gist
}

fail() {
  local msg=$1
  echo "ERROR: ${msg}" >&2
  exit 1
}

main $@
