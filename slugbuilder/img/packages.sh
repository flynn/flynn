#!/bin/bash

cp -r slugbuilder/builder /builder

# Explicitly number the buildpacks directory based on the order of buildpacks.txt
nl -nrz /builder/buildpacks.txt | awk '{print $2 "\t" $1}' | xargs -L 1 /builder/install-buildpack /builder/buildpacks

# allow custom buildpack install by unprivileged user
chmod ugo+w /builder/buildpacks
