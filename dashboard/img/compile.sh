#!/bin/bash

cd /app
cp -r /src/app/lib .
cp -r /src/app/vendor .
/src/app/compiler

cd /
/src/bin/go-bindata -nomemcopy -nocompress -pkg "main" app/build/...
