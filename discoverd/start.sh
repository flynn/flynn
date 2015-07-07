#!/bin/sh

exec /bin/discoverd -http-addr=":${PORT_0}" -notify="http://127.0.0.1:1113/host/discoverd" -wait-net-dns=true
