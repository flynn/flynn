#!/bin/bash

echo "cleaning apt cache"
apt-get autoremove
apt-get clean

echo "deleting old kernels"
cur_kernel=$(uname -r|sed 's/-*[a-z]//g'|sed 's/-386//g')
kernel_pkg="linux-(image|headers|ubuntu-modules|restricted-modules)"
meta_pkg="${kernel_pkg}-(generic|i386|server|common|rt|xen|ec2)"
apt-get purge -y $(dpkg -l | egrep $kernel_pkg | egrep -v "${cur_kernel}|${meta_pkg}" | awk '{print $2}')

# Removing leftover leases and persistent rules
echo "cleaning up dhcp leases"
rm /var/lib/dhcp/*

# Make sure Udev doesn't block our network
echo "cleaning up udev rules"
rm /etc/udev/rules.d/70-persistent-net.rules
mkdir /etc/udev/rules.d/70-persistent-net.rules
rm -rf /dev/.udev/
rm /lib/udev/rules.d/75-persistent-net-generator.rules

echo "Adding a 2 sec delay to the interface up, to make the dhclient happy"
echo "pre-up sleep 2" >> /etc/network/interfaces

# Zero out the free space to save space in the final image:
echo "Zeroing device to make space..."
dd if=/dev/zero of=/EMPTY bs=1M
rm -f /EMPTY
