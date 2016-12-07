#!/bin/bash

export DEBIAN_FRONTEND=noninteractive

apt-get update
apt-get dist-upgrade -y
apt-get -qy --fix-missing --force-yes install language-pack-en
update-locale LANG=en_US.UTF-8 LANGUAGE=en_US.UTF-8 LC_ALL=en_US.UTF-8
dpkg-reconfigure locales
apt-get -y install curl sudo
apt-key adv --keyserver hkp://keyserver.ubuntu.com:80 --recv B97B0AFCAA1A47F044F244A07FCC7D46ACCC4CF8
echo "deb http://apt.postgresql.org/pub/repos/apt/ trusty-pgdg main" >> /etc/apt/sources.list.d/postgresql.list
apt-get update
apt-get install -y -q \
  less \
  postgresql-9.5 \
  postgresql-contrib-9.5 \
  postgresql-9.5-pgextwlist \
  postgresql-9.5-plv8 \
  postgresql-9.5-postgis-2.3 \
  postgresql-9.5-pgrouting
apt-get clean
apt-get autoremove -y

echo "\set HISTFILE /dev/null" > /root/.psqlrc
