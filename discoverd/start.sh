#!/bin/sh

exec /bin/discoverd \
  -data-dir=/data \
  -host="${EXTERNAL_IP}" \
  -peers="${DISCOVERD_PEERS}" \
  -addr="${LISTEN_IP}:${PORT_0}" \
  -notify="http://${EXTERNAL_IP}:1113/host/discoverd" \
  -wait-net-dns=true
