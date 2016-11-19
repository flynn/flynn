#!/bin/bash
#
# A script to create ZFS device nodes
# (expected to be called by udevd inside a container)

set -e

if ! [[ -e "${DEVNAME}" ]]; then
  mknod "${DEVNAME}" "b" "${MAJOR}" "${MINOR}"
fi
