#!/bin/bash
#
# A script to start flynn-host inside a container.

# exit on error
set -e

# create /dev/zfs
mknod -m 0600 /dev/zfs c $(sed 's|:| |' /sys/class/misc/zfs/dev)

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
