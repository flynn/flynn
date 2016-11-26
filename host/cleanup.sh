#!/bin/bash
#
# A script to cleanup after flynn-host has exited inside a container.

set -ex

JOB_ID="$1"

mknod -m 0600 /dev/zfs c $(sed 's|:| |' /sys/class/misc/zfs/dev)
ln -nfs /proc/mounts /etc/mtab
zpool destroy "flynn-${JOB_ID}"
rm -rf "/var/lib/flynn/${JOB_ID}"
