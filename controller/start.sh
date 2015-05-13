#!/bin/sh

case $1 in
  controller) exec /bin/flynn-controller ;;
  scheduler)  exec /bin/flynn-scheduler ;;
  worker)  exec /bin/flynn-worker ;;
  *)
    echo "Usage: $0 {controller|scheduler|worker}"
    exit 2
    ;;
esac
