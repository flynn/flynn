#!/bin/bash

case $1 in
  mongodb)
    chown -R mongodb:mongodb /data
    chmod 0700 /data
    shift
    exec sudo \
      -u mongodb \
      -E -H \
      /bin/flynn-mongodb $*
    ;;
  api)
    shift
    exec /bin/flynn-mongodb-api $*
    ;;
  *)
    echo "Usage: $0 {mongodb|api}"
    exit 2
    ;;
esac
