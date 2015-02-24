#!/bin/bash
set -e -x

# init environment
export LC_ALL=C
mount -t proc none /proc

cleanup() {
  umount /proc
}
trap cleanup EXIT

# divert initctl and set rc.d policy so we don't start any daemons in the chroot
dpkg-divert --local --rename --add /sbin/initctl
ln -s /bin/true /sbin/initctl
echo $'#!/bin/sh\nexit 101' > /usr/sbin/policy-rc.d
chmod +x /usr/sbin/policy-rc.d

# set up ubuntu user
addgroup docker
addgroup fuse
adduser --disabled-password --gecos "" ubuntu
usermod -a -G sudo ubuntu
usermod -a -G docker ubuntu
usermod -a -G fuse ubuntu
echo %ubuntu ALL=NOPASSWD:ALL > /etc/sudoers.d/ubuntu
chmod 0440 /etc/sudoers.d/ubuntu
echo ubuntu:ubuntu | chpasswd

# set up fstab
echo "LABEL=rootfs / ext4 defaults 0 1" > /etc/fstab
echo "netfs /etc/network/interfaces.d 9p trans=virtio 0 0" >> /etc/fstab

# configure hosts and dns resolution
echo "127.0.0.1 localhost localhost.localdomain" > /etc/hosts
echo -e "nameserver 8.8.8.8\nnameserver 8.8.4.4" > /etc/resolv.conf

# enable universe
sed -i "s/^#\s*\(deb.*universe\)\$/\1/g" /etc/apt/sources.list

# use EC2 apt mirror as it's much quicker in CI
sed -i \
  "s/archive.ubuntu.com/us-west-1.ec2.archive.ubuntu.com/g" \
  /etc/apt/sources.list

# disable apt caching and add speedups
echo "force-unsafe-io" > /etc/dpkg/dpkg.cfg.d/02apt-speedup
cat >/etc/apt/apt.conf.d/no-cache <<EOF
DPkg::Post-Invoke {
  "rm -f \
    /var/cache/apt/archives/*.deb \
    /var/cache/apt/archives/partial/*.deb \
    /var/cache/apt/*.bin \
    || true";
};
APT::Update::Post-Invoke {
  "rm -f \
    /var/cache/apt/archives/*.deb \
    /var/cache/apt/archives/partial/*.deb \
    /var/cache/apt/*.bin \
    || true";
};
Dir::Cache::pkgcache "";
Dir::Cache::srcpkgcache "";
EOF
echo 'Acquire::Languages "none";' > /etc/apt/apt.conf.d/no-languages

# update packages
export DEBIAN_FRONTEND=noninteractive
apt-get update
apt-get install --install-recommends linux-generic-lts-utopic \
  -y \
  -o Dpkg::Options::="--force-confdef" \
  -o Dpkg::Options::="--force-confold"
apt-get dist-upgrade \
  -y \
  -o Dpkg::Options::="--force-confdef" \
  -o Dpkg::Options::="--force-confold"

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
apt-key adv \
  --keyserver hkp://keyserver.ubuntu.com:80 \
  --recv-keys 36A1D7869245C8950F966E92D8576A8BA88D21E9
echo deb https://get.docker.com/ubuntu docker main \
  > /etc/apt/sources.list.d/docker.list
apt-get update
apt-get install -y lxc-docker aufs-tools apparmor

# install flynn build dependencies: tup
apt-get install -y software-properties-common
apt-add-repository 'deb http://ppa.launchpad.net/titanous/tup/ubuntu trusty main'
apt-key adv \
  --keyserver keyserver.ubuntu.com \
  --recv 27947298A222DFA46E207200B34FBCAA90EA7F4E

# install flynn runtime dependencies: zfs
echo deb http://ppa.launchpad.net/zfs-native/stable/ubuntu trusty main \
  > /etc/apt/sources.list.d/zfs.list
apt-key adv \
  --keyserver keyserver.ubuntu.com \
  --recv E871F18B51E0147C77796AC81196BA81F6B0FC61

apt-get update
apt-get install -y \
  tup \
  fuse \
  build-essential \
  libdevmapper-dev \
  ubuntu-zfs \
  btrfs-tools \
  libvirt-dev \
  libvirt-bin \
  inotify-tools

# install flynn test dependencies: postgres
# (normally this is used via an appliance; this is for unit tests)
apt-get install -y postgresql postgresql-contrib
service postgresql start
sudo -u postgres createuser --superuser ubuntu
update-rc.d postgresql disable
service postgresql stop

# make tup suid root so that we can build in chroots
chmod ug+s /usr/bin/tup

# give ubuntu user access to tup fuse mounts
sed 's/#user_allow_other/user_allow_other/' -i /etc/fuse.conf

# install go
curl -L j.mp/godeb | tar xz
./godeb install 1.4.2
rm godeb

# install go-tuf
export GOPATH="$(mktemp --directory)"
trap "rm -rf ${GOPATH}" EXIT
go get github.com/flynn/go-tuf/cmd/tuf
mv "${GOPATH}/bin/tuf" /usr/bin/tuf

# allow the test runner to set TEST_RUNNER_AUTH_KEY
echo AcceptEnv TEST_RUNNER_AUTH_KEY >> /etc/ssh/sshd_config

# cleanup
apt-get autoremove -y
apt-get clean

# recreate resolv.conf symlink
ln -nsf ../run/resolvconf/resolv.conf /etc/resolv.conf

# remove the initctl diversion and rc.d policy so Upstart works in VMs
rm /usr/sbin/policy-rc.d
rm /sbin/initctl
dpkg-divert --rename --remove /sbin/initctl
