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
# install backported kernel, pin version to 3.19.0-39 to work around https://github.com/docker/docker/issues/18180 / https://github.com/flynn/flynn/issues/2365
apt-get install --install-recommends linux-headers-3.19.0-39 linux-headers-3.19.0-39-generic linux-image-3.19.0-39-generic linux-image-extra-3.19.0-39-generic \
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
  --keyserver hkp://p80.pool.sks-keyservers.net:80 \
  --recv-keys 58118E89F3A912897C070ADBF76221572C52609D
echo deb https://apt.dockerproject.org/repo ubuntu-trusty main \
  > /etc/apt/sources.list.d/docker.list
apt-get update
apt-get install -y docker-engine aufs-tools apparmor

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
  ubuntu-zfs \
  btrfs-tools \
  libvirt-dev \
  libvirt-bin \
  inotify-tools

# install flynn test dependencies: postgres
# (normally this is used via an appliance; this is for unit tests)
apt-get -qy --fix-missing --force-yes install language-pack-en
update-locale LANG=en_US.UTF-8 LANGUAGE=en_US.UTF-8 LC_ALL=en_US.UTF-8
dpkg-reconfigure locales
curl --fail --silent https://www.postgresql.org/media/keys/ACCC4CF8.asc | apt-key add -
echo "deb http://apt.postgresql.org/pub/repos/apt/ trusty-pgdg main" >> /etc/apt/sources.list.d/postgresql.list
apt-get update
apt-get install -y postgresql-9.4 postgresql-contrib-9.4
service postgresql start
sudo -u postgres createuser --superuser ubuntu
update-rc.d postgresql disable
service postgresql stop
apt-get install -y redis-server

# make tup suid root so that we can build in chroots
chmod ug+s /usr/bin/tup

# give ubuntu user access to tup fuse mounts
sed 's/#user_allow_other/user_allow_other/' -i /etc/fuse.conf

# install go
curl -L j.mp/godeb | tar xz
./godeb install 1.4.3
rm godeb

# install go-tuf
export GOPATH="$(mktemp --directory)"
trap "rm -rf ${GOPATH}" EXIT
go get github.com/flynn/go-tuf/cmd/tuf
mv "${GOPATH}/bin/tuf" /usr/bin/tuf
go get github.com/flynn/go-tuf/cmd/tuf-client
mv "${GOPATH}/bin/tuf-client" /usr/bin/tuf-client

# install go cover
go get golang.org/x/tools/cmd/cover

# allow the test runner to set TEST_RUNNER_AUTH_KEY
echo AcceptEnv TEST_RUNNER_AUTH_KEY >> /etc/ssh/sshd_config

# install Bats and jq for running script unit tests
tmpdir=$(mktemp --directory)
trap "rm -rf ${tmpdir}" EXIT
git clone https://github.com/sstephenson/bats.git "${tmpdir}/bats"
"${tmpdir}/bats/install.sh" "/usr/local"
curl -fsLo "/usr/local/bin/jq" "http://stedolan.github.io/jq/download/linux64/jq"
chmod +x "/usr/local/bin/jq"

# cleanup
apt-get autoremove -y
apt-get clean

# recreate resolv.conf symlink
ln -nsf ../run/resolvconf/resolv.conf /etc/resolv.conf

# remove the initctl diversion and rc.d policy so Upstart works in VMs
rm /usr/sbin/policy-rc.d
rm /sbin/initctl
dpkg-divert --rename --remove /sbin/initctl
