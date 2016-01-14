#!/bin/bash

case $1 in
  redis)
    shift
    exec /bin/flynn-redis $*
    ;;
  api)
    shift
    exec /bin/flynn-redis-api $*
    ;;
  *)
    echo "Usage: $0 {redis|api}"
    exit 2
    ;;
esac
