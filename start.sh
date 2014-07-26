#!/bin/bash

case $1 in
  mongo)
    shift
    /bin/flynn-mongodb $*
    ;;
    ;;
  *)
    echo "Usage: $0 {mongo}"
    exit 2
    ;;
esac
