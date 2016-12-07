#!/bin/bash

apt-get update
apt-get -qy install git curl squashfs-tools
apt-get clean

curl -fsSLo "/usr/local/bin/docker" "https://get.docker.com/builds/Linux/x86_64/docker-1.9.1"
chmod +x "/usr/local/bin/docker"

export HOME="/root"
git config --global "user.email" "test@flynn.io"
git config --global "user.name"  "Flynn Test"
