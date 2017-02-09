#!/bin/bash

export DEBIAN_FRONTEND=noninteractive

apt-get update
apt-get dist-upgrade -y
apt-get install -y curl software-properties-common
add-apt-repository ppa:chris-lea/redis-server
apt-get update
apt-get install -y redis-server
apt-get clean
apt-get autoremove -y
mkdir /data
