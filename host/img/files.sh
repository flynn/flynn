#!/bin/bash

mkdir -p /etc/ssl/certs
cp /src/bin/ca-certs.pem /etc/ssl/certs/ca-certs.pem
cp /src/bin/flynn-host   /usr/local/bin/flynn-host
cp /src/bin/flynn-init   /usr/local/bin/flynn-init
cp /src/zfs-mknod.sh     /usr/local/bin/zfs-mknod
cp /src/udev.rules       /lib/udev/rules.d/10-local.rules
cp /src/start.sh         /usr/local/bin/start-flynn-host.sh
cp /src/cleanup.sh       /usr/local/bin/cleanup-flynn-host.sh
