#!/bin/sh

start=$(date +%s)
dnsaddr=$(echo "$@" | sed -r 's/.*dns-addr=([0-9.]+):.*/\1/')

# Wait for flannel interface to show up, as we may have started before flannel.
while true; do
  ifconfig | grep -qF "$dnsaddr" && break || sleep 0.2
  elapsed=$(($(date +%s) - $start))
  if [ $elapsed -gt 60 ]; then
    echo "$dnsaddr did not appear within 60 seconds"
    exit 1
  fi
done

exec /bin/discoverd $*
