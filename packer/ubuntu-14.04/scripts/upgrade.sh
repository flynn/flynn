#!/bin/bash

apt-get update
apt-get dist-upgrade -y

echo "Rebooting the machine..."
reboot
sleep 60
