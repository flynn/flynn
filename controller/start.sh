#!/bin/sh

case $1 in
  controller) exec /bin/flynn-controller ;;
  scheduler)  exec /bin/flynn-scheduler ;;
  deployer)  exec /bin/flynn-deployer ;;
  *)
    echo "Usage: $0 {controller|scheduler|deployer}"
    exit 2
    ;;
esac
