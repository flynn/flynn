#!/bin/bash

chown -R postgres:postgres /data
sudo -u postgres -H PORT=$PORT DISCOVERD=$DISCOVERD /bin/flynn-postgres
