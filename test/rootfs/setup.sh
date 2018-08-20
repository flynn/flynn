#!/bin/bash
set -e -x

# init environment
export LC_ALL=C

# set up ubuntu user
addgroup docker
adduser --disabled-password --gecos "" ubuntu
usermod -a -G sudo ubuntu
usermod -a -G docker ubuntu
mkdir -p /etc/sudoers.d
echo %ubuntu ALL=NOPASSWD:ALL > /etc/sudoers.d/ubuntu
chmod 0440 /etc/sudoers.d/ubuntu
echo ubuntu:ubuntu | chpasswd

# set up fstab
echo "LABEL=rootfs / ext4 defaults 0 1" > /etc/fstab

# setup networking
cat > /etc/systemd/network/10-flynn.network <<EOF
[Match]
Name=en*

[Network]
DHCP=ipv4
EOF
systemctl enable systemd-networkd.service

# configure hosts and dns resolution
echo "127.0.0.1 localhost localhost.localdomain" > /etc/hosts
echo -e "nameserver 8.8.8.8\nnameserver 8.8.4.4" > /etc/resolv.conf

# enable universe
sed -i "s/^#\s*\(deb.*universe\)\$/\1/g" /etc/apt/sources.list

# use GCP apt mirror
sed -i \
  "s/archive.ubuntu.com/us-central1.gce.archive.ubuntu.com/g" \
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
# explicitly install linux 4.13 as the version of ZFS available on xenial is
# not compatible with linux 4.15 (the 'zfs' command just hangs)
#
# TODO: switch back to linux-generic-hwe-16.04 once ZFS works with the latest kernel
apt-get install --install-recommends linux-image-4.13.0-1019-gcp udev \
  -y \
  -o Dpkg::Options::="--force-confdef" \
  -o Dpkg::Options::="--force-confold"
apt-get dist-upgrade \
  -y \
  -o Dpkg::Options::="--force-confdef" \
  -o Dpkg::Options::="--force-confold"

# install ssh server and go deps
apt-get install -y apt-transport-https openssh-server git make curl

# install docker
apt-key adv \
  --keyserver hkp://p80.pool.sks-keyservers.net:80 \
  --recv-keys 58118E89F3A912897C070ADBF76221572C52609D
echo deb https://apt.dockerproject.org/repo ubuntu-xenial main \
  > /etc/apt/sources.list.d/docker.list
apt-get update
apt-get install -y "docker-engine" "aufs-tools"
systemctl disable docker

apt-get update

# install build dependencies
apt-get install -y \
  build-essential \
  zfsutils-linux \
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
  177F4010FE56CA3336300305F1656F24C74CD1D8 \
  4D1BB29D63D98E422B2113B19334A25F8507EFA5 \
  42F3E95A2C4F08279C4960ADD68FA50FEA312927 \
  136221EE520DDFAF0A905689B9316A7BC7917B12

# add repos
echo "deb http://apt.postgresql.org/pub/repos/apt/ xenial-pgdg main" >> /etc/apt/sources.list.d/postgresql.list
echo "deb http://sfo1.mirrors.digitalocean.com/mariadb/repo/10.1/ubuntu xenial main" >> /etc/apt/sources.list.d/mariadb.list
echo "deb http://repo.percona.com/apt xenial main" >> /etc/apt/sources.list.d/percona.list
echo "deb http://repo.mongodb.org/apt/ubuntu xenial/mongodb-org/3.2 multiverse" >> /etc/apt/sources.list.d/mongo-org-3.2.list
echo "deb http://ppa.launchpad.net/chris-lea/redis-server/ubuntu xenial main" >> /etc/apt/sources.list.d/redis.list

# update lists
apt-get update

# install packages
apt-get install -y postgresql-10 postgresql-contrib-10 redis-server mariadb-server percona-xtrabackup mongodb-org sudo net-tools

pg_ctlcluster --skip-systemctl-redirect 10-main start
sudo -u postgres createuser --superuser ubuntu
pg_ctlcluster --skip-systemctl-redirect 10-main -m fast stop

systemctl disable postgresql
systemctl disable mysql
systemctl disable mongod
systemctl disable redis-server

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
