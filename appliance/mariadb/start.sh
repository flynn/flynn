#!/bin/bash

case $1 in
  mariadb)
    chown -R mysql:mysql /data
    chmod 0700 /data
    shift
    exec sudo \
      -u mysql \
      -E -H \
      /bin/flynn-mariadb $*
    ;;
  api)
    shift
    exec /bin/flynn-mariadb-api $*
    ;;
  *)
    echo "Usage: $0 {mariadb|api}"
    exit 2
    ;;
esac
