#!/bin/bash

export DEBIAN_FRONTEND=noninteractive

apt-key adv --keyserver hkp://keyserver.ubuntu.com:80 --recv B97B0AFCAA1A47F044F244A07FCC7D46ACCC4CF8
echo "deb http://apt.postgresql.org/pub/repos/apt/ bionic-pgdg main" >> /etc/apt/sources.list.d/postgresql.list
apt-get update
apt-get dist-upgrade -y
apt-get -qy --fix-missing --force-yes install language-pack-en software-properties-common
update-locale LANG=en_US.UTF-8 LANGUAGE=en_US.UTF-8 LC_ALL=en_US.UTF-8
dpkg-reconfigure locales
apt-get -y install curl sudo
add-apt-repository ppa:timescale/timescaledb-ppa
apt-get update
apt-get install -y -q \
  less \
  postgresql-11 \
  postgresql-contrib-11 \
  postgresql-11-pgextwlist \
  postgresql-11-postgis-2.5 \
  postgresql-11-pgrouting \
  timescaledb-postgresql-11
apt-get clean
apt-get autoremove -y

echo "\set HISTFILE /dev/null" > /root/.psqlrc
