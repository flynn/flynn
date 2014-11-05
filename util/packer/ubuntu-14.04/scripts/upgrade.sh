#!/bin/bash

set -xeo pipefail

export DEBIAN_FRONTEND=noninteractive
apt-get update
apt-get dist-upgrade -y \
  -o Dpkg::Options::="--force-confdef" \
  -o Dpkg::Options::="--force-confold"

echo "Rebooting the machine..."
reboot
sleep 60
