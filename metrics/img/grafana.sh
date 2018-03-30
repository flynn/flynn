#!/bin/bash

URL="https://s3-us-west-2.amazonaws.com/grafana-releases/release/grafana-5.1.0.linux-x64.tar.gz"
SHA="7f74bea4a574d7b972684ac0e693f1d71ee2497a7ab035d83dc2623a6c509023"

curl -fsSLo /tmp/grafana.tar.gz "${URL}"
echo "${SHA}  /tmp/grafana.tar.gz" | shasum -a 256 -c -
mkdir -p /var/lib/grafana
tar xzf /tmp/grafana.tar.gz --strip-components=1 -C /var/lib/grafana
