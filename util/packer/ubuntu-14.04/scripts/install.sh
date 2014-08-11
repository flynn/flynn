#!/bin/bash
set -xeo pipefail

# enable memory and swap cgroup
perl -p -i -e 's/GRUB_CMDLINE_LINUX=""/GRUB_CMDLINE_LINUX="cgroup_enable=memory swapaccount=1"/g'  /etc/default/grub
/usr/sbin/update-grub

# add docker group and add vagrant to it
sudo groupadd docker
sudo usermod -a -G docker vagrant

# add the docker and tup gpg keys
apt-key adv --keyserver keyserver.ubuntu.com --recv 36A1D7869245C8950F966E92D8576A8BA88D21E9
apt-key adv --keyserver keyserver.ubuntu.com --recv E601AAF9486D3664

echo deb https://get.docker.io/ubuntu docker main > /etc/apt/sources.list.d/docker.list

apt-get update
apt-get install -y curl vim-tiny git mercurial bzr make lxc-docker linux-image-extra-$(uname -r) software-properties-common libdevmapper-dev btrfs-tools libvirt-dev ruby2.0 ruby2.0-dev

apt-add-repository 'deb http://ppa.launchpad.net/anatol/tup/ubuntu precise main'
apt-get update
apt-get install -y tup

gem2.0 install fpm --no-rdoc --no-ri

deb="flynn-host_0-${FLYNN_REV}_amd64.deb"
cd /tmp
wget https://s3.amazonaws.com/flynn/$deb
dpkg -i $deb || true
apt-get -f install -y

mkdir -p /var/lib/docker
flynn-release download /etc/flynn/version.json

# Disable container auto-restart when docker starts
sed -i 's/^#DOCKER_OPTS=.*/DOCKER_OPTS="-r=false"/' /etc/default/docker

# install Go
cd /tmp
wget j.mp/godeb
tar xvzf godeb
./godeb install
