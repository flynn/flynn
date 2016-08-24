#!/bin/bash
#
# A script to convert a legacy slug tarball to a Flynn squashfs image

# exit on failure
set -eo pipefail

# create a tmp dir
tmp="$(mktemp --directory)"
trap "rm -rf ${tmp}" EXIT

# extract the slug tarball from stdin into an "app" directory
mkdir "${tmp}/app"
cat | tar xz -C "${tmp}/app"

# create the artifact
/bin/create-artifact "${tmp}"
