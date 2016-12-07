#!/bin/bash

mkdir -p /builder
cp /src/builder/install-buildpack /builder/install-buildpack

# Explicitly number the buildpacks directory based on the order of buildpacks.txt
nl -nrz /src/builder/buildpacks.txt | awk '{print $2 "\t" $1}' | xargs -L 1 /builder/install-buildpack /builder/buildpacks

# allow custom buildpack install by unprivileged user
chmod ugo+w /builder/buildpacks

# install squashfs-tools for creating slug layers
apt-get install --yes squashfs-tools
