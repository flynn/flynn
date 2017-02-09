#!/bin/bash

mkdir -p /etc/ssl/certs
cp /src/bin/docker-receive  /bin/docker-receive
cp /src/bin/docker-artifact /bin/docker-artifact
cp /src/bin/docker-migrator /bin/docker-migrator
cp /src/bin/ca-certs.pem    /etc/ssl/certs/ca-certs.pem
