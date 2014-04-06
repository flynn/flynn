#!/bin/bash

echo "Input: $1"

case $1 in
  postgres)
    chown -R postgres:postgres /data
    sudo -u postgres -H EXTERNAL_IP=$EXTERNAL_IP PORT=$PORT DISCOVERD=$DISCOVERD /bin/flynn-postgres
    ;;
  api)
    /bin/flynn-postgres-api
    ;;
  *)
    echo "Usage: $0 {postgres|api}"
    exit 2
    ;;
esac
