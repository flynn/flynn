#!/bin/bash
set -xeo pipefail

apt-get update
apt-get dist-upgrade -y

echo "Rebooting the machine..."
reboot
sleep 60
