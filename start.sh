#!/bin/bash

chown -R postgres:postgres /data
sudo -u postgres -H EXTERNAL_IP=$EXTERNAL_IP PORT=$PORT DISCOVERD=$DISCOVERD /bin/flynn-postgres
