#!/bin/bash
set -e -x

# init environment
export LC_ALL=C
mount -t proc none /proc

function cleanup {
  umount /proc
}
trap cleanup EXIT

# set up ubuntu user
addgroup docker
adduser --disabled-password --gecos "" ubuntu
usermod -a -G sudo ubuntu
usermod -a -G docker ubuntu
echo %ubuntu ALL=NOPASSWD:ALL > /etc/sudoers.d/ubuntu
chmod 0440 /etc/sudoers.d/ubuntu
echo ubuntu:ubuntu | chpasswd

# set up fstab
echo "LABEL=rootfs / ext4 defaults 0 1" > /etc/fstab
echo "LABEL=dockerfs /var/lib/docker btrfs defaults 0 0" >> /etc/fstab
echo "netfs /etc/network/interfaces.d 9p trans=virtio 0 0" >> /etc/fstab

# configure hosts and dns resolution
echo "127.0.0.1 localhost localhost.localdomain" > /etc/hosts
echo -e "nameserver 8.8.8.8\nnameserver 8.8.4.4" > /etc/resolv.conf

# enable universe
sed -i 's/^#\s*\(deb.*universe\)$/\1/g' /etc/apt/sources.list

# disable apt caching and add speedups
echo 'force-unsafe-io' > /etc/dpkg/dpkg.cfg.d/02apt-speedup
cat >/etc/apt/apt.conf.d/no-cache <<EOF
DPkg::Post-Invoke {
  "rm -f /var/cache/apt/archives/*.deb /var/cache/apt/archives/partial/*.deb /var/cache/apt/*.bin || true";
};
APT::Update::Post-Invoke {
  "rm -f /var/cache/apt/archives/*.deb /var/cache/apt/archives/partial/*.deb /var/cache/apt/*.bin || true";
};
Dir::Cache::pkgcache "";
Dir::Cache::srcpkgcache "";
EOF
echo 'Acquire::Languages "none";' > /etc/apt/apt.conf.d/no-languages

# update packages
export DEBIAN_FRONTEND=noninteractive
apt-get update
apt-get dist-upgrade -y -o Dpkg::Options::='--force-confdef' -o Dpkg::Options::='--force-confold'
apt-get install linux-generic-lts-trusty -y -o Dpkg::Options::='--force-confdef' -o Dpkg::Options::='--force-confold'

# install ssh server and go deps
apt-get install -y apt-transport-https openssh-server mercurial git make curl
rm /etc/ssh/ssh_host_*

# add script that regenerates missing ssh host keys on boot
cat >/etc/init/ssh-hostkeys.conf <<EOF
start on starting ssh

script
  test -f /etc/ssh/ssh_host_dsa_key || dpkg-reconfigure openssh-server
end script
EOF

# install docker
# apparmor is required - see https://github.com/dotcloud/docker/issues/4734
apt-key adv --keyserver hkp://keyserver.ubuntu.com:80 --recv-keys 36A1D7869245C8950F966E92D8576A8BA88D21E9
echo deb https://get.docker.io/ubuntu docker main > /etc/apt/sources.list.d/docker.list
apt-get update
apt-get install -y lxc-docker-0.10.0 aufs-tools apparmor

# install go
curl -L j.mp/godeb | tar xz
./godeb install
rm godeb

# install godep
mkdir /gopkg
export GOPATH=/gopkg
go get github.com/tools/godep
mv /gopkg/bin/godep /usr/bin
rm -rf /gopkg

# cleanup
apt-get autoremove -y
apt-get clean

# recreate resolv.conf symlink
ln -nsf ../run/resolvconf/resolv.conf /etc/resolv.conf
