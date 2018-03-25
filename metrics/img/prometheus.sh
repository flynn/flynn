#!/bin/bash

URL="https://github.com/prometheus/prometheus/releases/download/v2.2.1/prometheus-2.2.1.linux-amd64.tar.gz"
SHA="ec1798dbda1636f49d709c3931078dc17eafef76c480b67751aa09828396cf31"

curl -fsSLo /tmp/prometheus.tar.gz "${URL}"
echo "${SHA}  /tmp/prometheus.tar.gz" | shasum -a 256 -c -
mkdir -p /var/lib/prometheus
tar xzf /tmp/prometheus.tar.gz --strip-components=1 -C /var/lib/prometheus
