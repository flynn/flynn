#!/bin/sh

case $1 in
  controller)
    exec /bin/flynn-controller
    ;;
  scheduler)
    exec /bin/flynn-scheduler
    ;;
  *)
    echo "Usage: $0 {controller|scheduler}"
    exit 2
    ;;
esac
