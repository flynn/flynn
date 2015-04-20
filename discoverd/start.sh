#!/bin/sh

# Wait for bridge interface to show up, as we may have started before the interface is configured
iface=flynnbr0
start=$(date +%s)
while true; do
  ip=$(ifconfig ${iface} | grep "inet addr:" | cut -d: -f2 | cut -d" " -f0)
  [ -n "${ip}" ] && break || sleep 0.2

  elapsed=$(($(date +%s) - ${start}))
  if [ ${elapsed} -gt 60 ]; then
    echo "${iface} did not appear within 60 seconds"
    exit 1
  fi
done

exec /bin/discoverd -http-addr=:${PORT_0} \
                    -dns-addr=${ip}:${PORT_1} \
                    -recursors=${DNS_RECURSORS} \
                    -etcd=${ETCD_ADDRS} \
					-notify="http://${ip}:1113/host/discoverd"
