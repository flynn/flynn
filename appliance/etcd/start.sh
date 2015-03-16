#!/bin/sh

if [ -z "${ETCD_INITIAL_CLUSTER}" ] && [ -z "${ETCD_DISCOVERY}" ]; then
  initial="-initial-cluster=default=http://${EXTERNAL_IP}:${PORT_1}"
fi

exec /bin/etcd -data-dir=/data \
               -advertise-client-urls=http://${EXTERNAL_IP}:${PORT_0} \
               -listen-client-urls=http://0.0.0.0:${PORT_0} \
               -initial-advertise-peer-urls=http://${EXTERNAL_IP}:${PORT_1} \
               -listen-peer-urls=http://0.0.0.0:${PORT_1} \
               "${initial}"
