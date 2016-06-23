#!/bin/bash -e

# Create temporary directory for dump
TMPFILE=`mktemp -d /tmp/dump.XXXXXXXXXX`
trap "rm -rf ${TMPFILE}" EXIT

# Passthrough arguments and dump to temporary path.
/usr/bin/mongodump $@ -o $TMPFILE

# Archive dump directory, gzip, & write to STDOUT.
cd $TMPFILE
tar -cf - *
