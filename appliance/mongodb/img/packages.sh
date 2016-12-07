#!/bin/bash

export DEBIAN_FRONTEND=noninteractive

apt-key adv --keyserver hkp://keyserver.ubuntu.com:80 --recv 42F3E95A2C4F08279C4960ADD68FA50FEA312927
echo "deb http://repo.mongodb.org/apt/ubuntu trusty/mongodb-org/3.2 multiverse" > /etc/apt/sources.list.d/mongodb-org-3.2.list
apt-get update
apt-get install -y sudo mongodb-org
apt-get clean
apt-get autoremove -y
