#!/bin/bash
set -e -x

# init environment
export LC_ALL=C
mount -t proc none /proc
mount -t sysfs sysfs /sys

cleanup() {
  umount /sys
  umount /proc
}
trap cleanup EXIT

# divert initctl and set rc.d policy so we don't start any daemons in the chroot
dpkg-divert --local --rename --add /sbin/initctl
ln -s /bin/true /sbin/initctl
echo $'#!/bin/sh\nexit 101' > /usr/sbin/policy-rc.d
chmod +x /usr/sbin/policy-rc.d

# NOTE: chfn & chpasswd must be run in a network namespace to avoid PAM related
#       errors (see https://github.com/docker/docker/issues/5704), but we can't
#       just run the whole script in a network namespace (because then we would
#       have no network!), so hack just those programs to run in a namespace.
#
#       This behaviour was witnessed with kernel 3.13.0-77-generic, so when CI
#       is running on a more recent kernel, try removing this hack.
ip netns add rootfs
mv /usr/bin/chfn{,.orig}
mv /usr/sbin/chpasswd{,.orig}
echo -e '#!/bin/bash\nip netns exec rootfs $0.orig "$@"' > /usr/bin/netns-hack
chmod +x /usr/bin/netns-hack
ln -s /usr/bin/netns-hack /usr/bin/chfn
ln -s /usr/bin/netns-hack /usr/sbin/chpasswd
apt-mark hold passwd
netns_cleanup() {
  apt-mark unhold passwd
  rm /usr/bin/netns-hack
  mv /usr/bin/chfn{.orig,}
  mv /usr/sbin/chpasswd{.orig,}
  ip netns delete rootfs
}
trap netns_cleanup EXIT

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
apt-get install --install-recommends linux-generic-lts-xenial \
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
# pin docker version due to https://github.com/flynn/flynn/issues/2459
apt-key adv \
  --keyserver hkp://p80.pool.sks-keyservers.net:80 \
  --recv-keys 58118E89F3A912897C070ADBF76221572C52609D
echo deb https://apt.dockerproject.org/repo ubuntu-trusty main \
  > /etc/apt/sources.list.d/docker.list
apt-get update
apt-get install -y "docker-engine=1.9.1-0~trusty" "aufs-tools" "apparmor"
apt-mark hold docker-engine

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
  inotify-tools \
  libsasl2-dev \
  libseccomp-dev \
  squashfs-tools \
  pkg-config

# install flynn test dependencies: postgres, redis, mariadb
# (normally these are used via appliances; install locally for unit tests)
apt-get -qy --fix-missing --force-yes install language-pack-en
update-locale LANG=en_US.UTF-8 LANGUAGE=en_US.UTF-8 LC_ALL=en_US.UTF-8
dpkg-reconfigure locales

# add keys
apt-key adv --recv-keys --keyserver hkp://keyserver.ubuntu.com:80 \
  B97B0AFCAA1A47F044F244A07FCC7D46ACCC4CF8 \
  199369E5404BD5FC7D2FE43BCBCB082A1BB943DB \
  4D1BB29D63D98E422B2113B19334A25F8507EFA5 \
  42F3E95A2C4F08279C4960ADD68FA50FEA312927 \
  136221EE520DDFAF0A905689B9316A7BC7917B12

# add repos
echo "deb http://apt.postgresql.org/pub/repos/apt/ trusty-pgdg main" >> /etc/apt/sources.list.d/postgresql.list
echo "deb http://sfo1.mirrors.digitalocean.com/mariadb/repo/10.1/ubuntu trusty main" >> /etc/apt/sources.list.d/mariadb.list
echo "deb http://repo.percona.com/apt trusty main" >> /etc/apt/sources.list.d/percona.list
echo "deb http://repo.mongodb.org/apt/ubuntu trusty/mongodb-org/3.2 multiverse" >> /etc/apt/sources.list.d/mongo-org-3.2.list
echo "deb http://ppa.launchpad.net/chris-lea/redis-server/ubuntu trusty main" >> /etc/apt/sources.list.d/redis.list

# update lists
apt-get update

# install packages
apt-get install -y postgresql-9.5 postgresql-contrib-9.5 redis-server mariadb-server percona-xtrabackup mongodb-org

# setup postgres
service postgresql start
sudo -u postgres createuser --superuser ubuntu
update-rc.d postgresql disable
service postgresql stop

# setup mariadb
update-rc.d mysql disable
service mysql stop

# setup mongodb
echo "manual" > /etc/init/mongod.override
stop mongod

# setup redis
update-rc.d redis-server disable
service redis-server stop

# make tup suid root so that we can build in chroots
chmod ug+s /usr/bin/tup

# give ubuntu user access to tup fuse mounts
sed 's/#user_allow_other/user_allow_other/' -i /etc/fuse.conf

# install go
curl https://godeb.s3.amazonaws.com/godeb-amd64.tar.gz | tar xz
./godeb install 1.9.2
rm godeb

# setup tmpdir
tmpdir=$(mktemp --directory)
trap "rm -rf ${tmpdir}" EXIT

# install go-tuf
export GOPATH="${tmpdir}/gopath"
mkdir ${GOPATH}
go get github.com/flynn/go-tuf/cmd/tuf
mv "${GOPATH}/bin/tuf" /usr/bin/tuf
go get github.com/flynn/go-tuf/cmd/tuf-client
mv "${GOPATH}/bin/tuf-client" /usr/bin/tuf-client

# install go cover
go get golang.org/x/tools/cmd/cover

# allow the test runner to set certain environment variables
echo AcceptEnv TEST_RUNNER_AUTH_KEY BLOBSTORE_S3_CONFIG BLOBSTORE_GCS_CONFIG BLOBSTORE_AZURE_CONFIG >> /etc/ssh/sshd_config

# install Bats and jq for running script unit tests
git clone https://github.com/sstephenson/bats.git "${tmpdir}/bats"
"${tmpdir}/bats/install.sh" "/usr/local"
curl -fsLo "/usr/local/bin/jq" "https://github.com/stedolan/jq/releases/download/jq-1.5/jq-linux64"
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
