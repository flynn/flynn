#!/bin/bash
set -xeo pipefail

# Set up sudo
if [[ ! -f /etc/sudoers.d/vagrant ]]; then
  echo "%vagrant ALL=(ALL) NOPASSWD:ALL" > /etc/sudoers.d/vagrant
  chmod 0440 /etc/sudoers.d/vagrant
fi

# Installing vagrant keys
if [[ ! -f /home/vagrant/.ssh/authorized_keys ]]; then
  mkdir /home/vagrant/.ssh
  chmod 700 /home/vagrant/.ssh
  cd /home/vagrant/.ssh
  wget --no-check-certificate 'https://raw.github.com/mitchellh/vagrant/master/keys/vagrant.pub' -O authorized_keys
  chmod 600 /home/vagrant/.ssh/authorized_keys
  chown -R vagrant /home/vagrant/.ssh
fi

# Install NFS for Vagrant
apt-get update
apt-get install -y nfs-common
