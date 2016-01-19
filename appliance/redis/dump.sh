#!/bin/bash

# Passthrough arguments and dump RDB to temporary location.
TMPFILE=`mktemp /tmp/dump.XXXXXXXXXX`
/usr/bin/redis-cli $@ --rdb $TMPFILE

# Write dump to STDOUT.
cat $TMPFILE

# Remove dump file.
rm $TMPFILE
