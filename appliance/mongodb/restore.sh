#!/bin/bash -e

# Extract dump into a temporary directory.
TMPFILE=`mktemp -d /tmp/dump.XXXXXXXXXX`
mkdir -p $TMPFILE
trap "rm -rf ${TMPFILE}" EXIT

# Extract tar archive into temporary directory.
tar -x -C $TMPFILE <&0

# Restore from temporary directory into database.
/usr/bin/mongorestore $@ $TMPFILE/*
