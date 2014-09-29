#!/bin/bash
set -xeo pipefail

echo "cleaning apt cache"
apt-get autoremove
apt-get clean

echo "deleting old kernels"
cur_kernel=$(uname -r|sed 's/-*[a-z]//g'|sed 's/-386//g')
kernel_pkg="linux-(image|headers|ubuntu-modules|restricted-modules)"
meta_pkg="${kernel_pkg}-(generic|i386|server|common|rt|xen|ec2|virtual)"
export DEBIAN_FRONTEND=noninteractive
apt-get purge -y $(dpkg -l | egrep $kernel_pkg | egrep -v "${cur_kernel}|${meta_pkg}" | awk '{print $2}')
