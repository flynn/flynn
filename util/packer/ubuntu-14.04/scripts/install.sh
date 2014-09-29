#!/bin/bash
set -xeo pipefail

# enable memory and swap cgroup
perl -p -i -e 's/GRUB_CMDLINE_LINUX=""/GRUB_CMDLINE_LINUX="cgroup_enable=memory swapaccount=1"/g'  /etc/default/grub
/usr/sbin/update-grub

# add docker group and add the current user to it
groupadd docker
usermod -a -G docker "${SUDO_USER}"

# add the docker, tup and flynn gpg keys
apt-key adv --keyserver keyserver.ubuntu.com --recv 36A1D7869245C8950F966E92D8576A8BA88D21E9
apt-key adv --keyserver keyserver.ubuntu.com --recv E601AAF9486D3664
apt-key adv --keyserver keyserver.ubuntu.com --recv BC79739C507A9B53BB1B0E7D820A5489998D827B

echo deb https://get.docker.io/ubuntu docker main > /etc/apt/sources.list.d/docker.list
echo deb https://dl.flynn.io/ubuntu flynn main > /etc/apt/sources.list.d/flynn.list
echo deb http://ppa.launchpad.net/anatol/tup/ubuntu precise main > /etc/apt/sources.list.d/tup.list

apt-get update

packages=(
  "btrfs-tools"
  "bzr"
  "curl"
  "git"
  "libdevmapper-dev"
  "libvirt-dev"
  "linux-image-extra-$(uname -r)"
  "lxc-docker"
  "make"
  "mercurial"
  "ruby2.0"
  "ruby2.0-dev"
  "tup"
  "vim-tiny"
)

if [[ -n "${FLYNN_DEB_URL}" ]]; then
  # If we are manually installing the deb, we need to also
  # manually install explicit dependencies of flynn-host
  packages+=(
    "aufs-tools"
    "iptables"
    "libvirt-bin"
  )
else
  packages+=("flynn-host")
fi

apt-get install -y ${packages[@]}

if [[ -n "${FLYNN_DEB_URL}" ]]; then
  curl "${FLYNN_DEB_URL}" > /tmp/flynn-host.deb
  dpkg -i /tmp/flynn-host.deb
  rm /tmp/flynn-host.deb
fi

gem2.0 install fpm --no-rdoc --no-ri

mkdir -p /var/lib/docker
flynn-release download /etc/flynn/version.json

# Disable container auto-restart when docker starts
sed -i 's/^#DOCKER_OPTS=.*/DOCKER_OPTS="-r=false"/' /etc/default/docker

# install Go
cd /tmp
wget j.mp/godeb
tar xvzf godeb
./godeb install
