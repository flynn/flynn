#!/bin/bash

set -e

cp -R dashboard /app
cd /app

yarn install
yarn run build

go-bindata -nomemcopy -nocompress -pkg "main" ./build/...
