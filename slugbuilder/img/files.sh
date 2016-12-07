#!/bin/bash

mkdir -p /builder
cp /src/convert-legacy-slug.sh /bin/convert-legacy-slug.sh
cp /src/bin/create-artifact    /bin/create-artifact
cp /src/bin/migrator           /bin/slug-migrator
cp /src/builder/build.sh       /builder/build.sh
cp /src/builder/create-user.sh /builder/create-user.sh
