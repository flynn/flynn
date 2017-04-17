#!/bin/bash

set -xeo pipefail

source /etc/lsb-release
export DEBIAN_FRONTEND=noninteractive

main() {
  disable_release_upgrader
  stop_cron
  setup_apt_sources
  upgrade_packages

  echo "Rebooting the machine..."
  reboot
  sleep 60
}

disable_release_upgrader() {
  sed -i 's/^Prompt=.*$/Prompt=never/' /etc/update-manager/release-upgrades
}

stop_cron() {
  # cron can run apt/dpkg commands that will disrupt our tasks
  case "${DISTRIB_RELEASE}" in
    14.04)
      service cron stop
      ;;
    16.04)
      systemctl stop cron
      ;;
  esac
}

setup_apt_sources() {
  cat << EOF > /etc/apt/sources.list
  deb http://archive.ubuntu.com/ubuntu ${DISTRIB_CODENAME} main universe
  deb-src http://archive.ubuntu.com/ubuntu ${DISTRIB_CODENAME} main universe
  deb http://archive.ubuntu.com/ubuntu ${DISTRIB_CODENAME}-updates main universe
  deb-src http://archive.ubuntu.com/ubuntu ${DISTRIB_CODENAME}-updates main universe
  deb http://security.ubuntu.com/ubuntu ${DISTRIB_CODENAME}-security main universe
  deb-src http://security.ubuntu.com/ubuntu ${DISTRIB_CODENAME}-security main universe
EOF

  # ensure cloud-init doesn't overwrite our apt sources
  if [[ -f /etc/cloud/cloud.cfg ]]; then
    grep -q '^apt_preserve_sources_list:' /etc/cloud/cloud.cfg &&
    sed -i 's/^apt_preserve_sources_list.*/apt_preserve_sources_list: true/' /etc/cloud/cloud.cfg ||
    echo 'apt_preserve_sources_list: true' >> /etc/cloud/cloud.cfg
  fi

  # clear existing apt lists
  rm -rf /var/lib/apt/lists/*

  apt-get update
}

upgrade_packages() {
  apt-get install --install-recommends linux-generic-hwe-16.04 \
    -y \
    -o Dpkg::Options::="--force-confdef" \
    -o Dpkg::Options::="--force-confold"

  apt-get autoremove -y

  apt-get dist-upgrade -y \
    -o Dpkg::Options::="--force-confdef" \
    -o Dpkg::Options::="--force-confold"
}

main $@
