#!/bin/bash

for pid in $(ps faux | grep '/opt/flynn-test/build/vmlinuz \-append' | grep -v grep | awk '{print $2}'); do
  echo killing $pid
  kill $pid
done

sleep 1

for tap in $(ifconfig | grep flynntap | awk '{print $1}'); do
  echo removing $tap
  sudo ip tuntap del mode tap $tap
done

for br in $(ifconfig | grep flynnbr | awk '{print $1}'); do
  echo removing $br
  sudo ip link set $br down
  sudo ip link del $br
done

for br in $(sudo ip link | grep flynnbr | awk '{print $2}' | cut -d : -f 1); do
  echo deleting $br
  sudo brctl delbr
done

rm -rf /opt/flynn-test/build/rootfs-*
rm -rf /opt/flynn-test/build/netfs-*
rm -rf /opt/flynn-test/build/build-log*

echo done
