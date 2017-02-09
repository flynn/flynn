#!/bin/bash

mkdir -p /etc/ssl/certs /etc/flynn-controller

cp /src/bin/flynn-controller /bin/flynn-controller
cp /src/bin/flynn-scheduler  /bin/flynn-scheduler
cp /src/bin/flynn-worker     /bin/flynn-worker
cp /src/bin/ca-certs.pem     /etc/ssl/certs/ca-certs.pem
cp /src/start.sh             /bin/start-flynn-controller
cp -r /src/bin/jsonschema    /etc/flynn-controller
