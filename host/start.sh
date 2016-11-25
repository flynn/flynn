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
  --volpath    "${DIR}/volumes"
  --log-dir    "${DIR}/logs"
  --zpool-name "${ZPOOL}"
  --no-resurrect
)

if [[ -n "${DISCOVERY_SERVICE}" ]]; then
  ARGS+=(
    --discovery-service "${DISCOVERY_SERVICE}"
  )
fi

# start flynn-host in the background and just block so that the container
# doesn't exit if flynn-host is updated in-place
PIDFILE="/tmp/flynn-host.pid"
start-stop-daemon \
  --start \
  --background \
  --no-close \
  --pidfile "${PIDFILE}" \
  --make-pidfile \
  --exec "/usr/local/bin/flynn-host" \
  -- \
  "daemon" \
  ${ARGS[@]}

cleanup() {
  set -x

  start-stop-daemon \
    --stop \
    --oknodo \
    --retry 5 \
    --pidfile "${PIDFILE}"

  flynn-host destroy-volumes --volpath "${DIR}/volumes" --include-data

  zpool destroy "${ZPOOL}"

  rm -rf "${DIR}"

  exit
}
trap cleanup TERM

# sleep in the background so we can catch the SIGTERM
sleep infinity &
wait
