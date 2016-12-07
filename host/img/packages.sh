#!/bin/bash

apt-get update
apt-get install --yes zfsutils-linux iptables udev net-tools iproute2
apt-get clean
