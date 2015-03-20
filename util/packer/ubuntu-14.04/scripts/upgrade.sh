#!/bin/bash

set -xeo pipefail

export DEBIAN_FRONTEND=noninteractive

# cron can run apt/dpkg commands that will disrupt our tasks
service cron stop

apt-get update

if [[ "${PACKER_BUILDER_TYPE}" == "virtualbox-ovf" ]]; then
  # Remove the pre-installed guest additions, as the kernel install may not be compatible with them and we'll be
  # installing our own copy anyway
  apt-get remove --purge -y virtualbox-guest-dkms virtualbox-guest-utils virtualbox-guest-x11
fi

apt-get install --install-recommends linux-generic-lts-utopic \
  -y \
  -o Dpkg::Options::="--force-confdef" \
  -o Dpkg::Options::="--force-confold"

apt-get autoremove -y

apt-get dist-upgrade -y \
  -o Dpkg::Options::="--force-confdef" \
  -o Dpkg::Options::="--force-confold"

echo "Rebooting the machine..."
reboot
sleep 60
