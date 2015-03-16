#!/bin/bash

if [ $ROUTER_IP != "127.0.0.1" ]
then
  dashboard_domain=$(echo "$URL" | sed 's/^https*:\/\///' | tr -d '\r\n')
  echo "$ROUTER_IP $dashboard_domain" >> /etc/hosts
fi

casperjs test /test/dashboard-app.js --ignore-ssl-errors=true
