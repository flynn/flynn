#!/bin/bash
#
# A script to cleanup after flynn-host has exited inside a container.

set -ex

JOB_ID="$1"

zpool destroy "flynn-${JOB_ID}"
rm -rf "/var/lib/flynn/${JOB_ID}"
