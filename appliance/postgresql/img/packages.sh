#!/bin/bash

export DEBIAN_FRONTEND=noninteractive

apt-get update
apt-get dist-upgrade -y
apt-get -qy --fix-missing --force-yes install language-pack-en software-properties-common
update-locale LANG=en_US.UTF-8 LANGUAGE=en_US.UTF-8 LC_ALL=en_US.UTF-8
dpkg-reconfigure locales
apt-get -y install curl sudo
apt-key adv --keyserver hkp://keyserver.ubuntu.com:80 --recv B97B0AFCAA1A47F044F244A07FCC7D46ACCC4CF8
echo "deb http://apt.postgresql.org/pub/repos/apt/ trusty-pgdg main" >> /etc/apt/sources.list.d/postgresql.list
add-apt-repository ppa:timescale/timescaledb-ppa
apt-get update
apt-get install -y -q \
  less \
  postgresql-10 \
  postgresql-contrib-10 \
  postgresql-10-pgextwlist \
  postgresql-10-plv8 \
  postgresql-10-postgis-2.4 \
  postgresql-10-pgrouting \
  timescaledb-postgresql-10
apt-get clean
apt-get autoremove -y

# install the pg_prometheus extension
URL="https://github.com/timescale/pg_prometheus/archive/0.2.tar.gz"
SHA="f652679cd3d385ded2a0666427faeb00862e296d7971c5b4585d4828e5ef19ab"
curl -fsSLo /tmp/pg_prometheus.tar.gz "${URL}"
echo "${SHA}  /tmp/pg_prometheus.tar.gz" | shasum -a 256 -c -
mkdir -p /tmp/pg_prometheus
tar xzf /tmp/pg_prometheus.tar.gz --strip-components=1 -C /tmp/pg_prometheus
apt-get install -y make gcc patch
make -C /tmp/pg_prometheus install
apt-get remove -y make gcc patch
apt-get clean
apt-get autoremove -y

echo "\set HISTFILE /dev/null" > /root/.psqlrc
