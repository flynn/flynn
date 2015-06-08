#!/bin/sh

exec /bin/discoverd -data-dir=/data -host="${EXTERNAL_IP}" -peers="${DISCOVERD_PEERS}" -raft-addr=":${PORT_0}" -http-addr=":${PORT_1}" -notify="http://127.0.0.1:1113/host/discoverd" -wait-net-dns=true
