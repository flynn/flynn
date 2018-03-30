#!/bin/sh

exec /var/lib/grafana/bin/grafana-server \
  --config   /etc/grafana.ini \
  --homepath /var/lib/grafana
