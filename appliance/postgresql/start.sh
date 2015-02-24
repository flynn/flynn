#!/bin/bash

case $1 in
  postgres)
    chown -R postgres:postgres /data
    shift
    exec sudo \
      -u postgres \
	  -E -H \
      /bin/flynn-postgres $*
    ;;
  api)
    shift
    exec /bin/flynn-postgres-api $*
    ;;
  *)
    echo "Usage: $0 {postgres|api}"
    exit 2
    ;;
esac
