#!/bin/bash

set -xeo pipefail

export DEBIAN_FRONTEND=noninteractive

# cron can run apt/dpkg commands that will disrupt our tasks
service cron stop

# setup system apt sources
cat << EOF > /etc/apt/sources.list
deb http://archive.ubuntu.com/ubuntu trusty main universe
deb-src http://archive.ubuntu.com/ubuntu trusty main universe
deb http://archive.ubuntu.com/ubuntu trusty-updates main universe
deb-src http://archive.ubuntu.com/ubuntu trusty-updates main universe
deb http://security.ubuntu.com/ubuntu trusty-security main universe
deb-src http://security.ubuntu.com/ubuntu trusty-security main universe
EOF

# ensure cloud-init doesn't overwrite our apt sources
if [[ -f /etc/cloud/cloud.cfg ]]; then
  grep -q '^apt_preserve_sources_list:' /etc/cloud/cloud.cfg &&
  sed -i 's/^apt_preserve_sources_list.*/apt_preserve_sources_list: true/' /etc/cloud/cloud.cfg ||
  echo 'apt_preserve_sources_list: true' >> /etc/cloud/cloud.cfg
fi

# clear apt lists
rm -rf /var/lib/apt/lists/*

apt-get update

if [[ "${PACKER_BUILDER_TYPE}" == "virtualbox-ovf" ]]; then
  # Remove the pre-installed guest additions, as the kernel install may not be compatible with them and we'll be
  # installing our own copy anyway
  apt-get remove --purge -y virtualbox-guest-dkms virtualbox-guest-utils virtualbox-guest-x11
fi

apt-get install --install-recommends linux-generic-lts-xenial \
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
