#!/bin/bash

case $1 in
  controller)
    /bin/flynn-controller
    ;;
  scheduler)
    /bin/flynn-scheduler
    ;;
  *)
    echo "Usage: $0 {controller|scheduler}"
    exit 2
    ;;
esac
