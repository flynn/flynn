#!/bin/bash

cp -r dashboard/app/lib    /app
cp -r dashboard/app/vendor /app

cd /app
/bin/dashboard-compile

cd /
go-bindata -nomemcopy -nocompress -pkg "main" app/build/...
