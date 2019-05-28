#!/bin/bash

export DEBIAN_FRONTEND=noninteractive

apt-get update
apt-get install -y software-properties-common apt-transport-https
apt-key adv --recv-keys --keyserver hkp://keyserver.ubuntu.com:80 \
  4D1BB29D63D98E422B2113B19334A25F8507EFA5 \
  177F4010FE56CA3336300305F1656F24C74CD1D8 \

add-apt-repository 'deb http://nyc2.mirrors.digitalocean.com/mariadb/repo/10.1/ubuntu bionic main'
add-apt-repository 'deb http://repo.percona.com/apt bionic main'
apt-get update
apt-get install -y sudo
apt-get install -y mariadb-server percona-xtrabackup
apt-get clean
apt-get autoremove -y
