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

echo "\set HISTFILE /dev/null" > /root/.psqlrc
