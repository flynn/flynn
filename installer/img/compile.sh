#!/bin/bash

cd /app
cp -r /src/app/src .
cp -r /src/app/vendor .
/src/app/compiler

cd /
/src/bin/go-bindata -nomemcopy -nocompress -pkg "installer" app/build/...
