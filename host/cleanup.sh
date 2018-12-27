#!/bin/bash
#
# A script to cleanup after flynn-host has exited inside a container.

set -e

JOB_ID="$1"
ZPOOL="flynn-${JOB_ID}"
HOST_DIR="/var/lib/flynn/${JOB_ID}"

# try multiple times to destroy the zpool in case it's still busy
for i in $(seq 10); do
  echo "destroying zpool: ${ZPOOL}"
  if zpool destroy "${ZPOOL}"; then
    break
  fi
  sleep 1
done

echo "removing host dir: ${HOST_DIR}"
rm -rf "${HOST_DIR}"
