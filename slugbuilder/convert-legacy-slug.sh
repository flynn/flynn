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

# create the "flynn" user
source "/builder/create-user.sh"

# update file ownership
chown -R "${USER_UID}:${USER_GID}" "${tmp}/app"

# import user information
mkdir -p "${tmp}/etc"
cp "/etc/passwd" "${tmp}/etc/passwd"
cp "/etc/group" "${tmp}/etc/group"

# ensure the root dir can be searched
chmod 755 "${tmp}"

# create the artifact
/bin/create-artifact \
  --dir "${tmp}" \
  --uid "${USER_UID}" \
  --gid "${USER_GID}"
