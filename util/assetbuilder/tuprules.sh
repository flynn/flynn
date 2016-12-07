#!/bin/bash

TARGET=$1

cat <<RULES
: |> \$(GO) build -o bin/go-bindata ../vendor/github.com/jteeuwen/go-bindata/go-bindata |> bin/go-bindata
: |> \$(GO) build -o app/compiler ./app |> app/compiler
RULES

# build the TARGET-builder image
cat <<RULES
: \$(ROOT)/util/assetbuilder/img/builder.sh |> !cp |> img/builder.sh
: img/builder.sh app/Gemfile app/Gemfile.lock |> !build-layer-cedarish |> layer/builder.json
: layer/builder.json | \$(ROOT)/<builder> \$(ROOT)/util/cedarish/<image> |> ^ build-artifact ${TARGET}-builder^ \$(ROOT)/builder/bin/flynn-build artifact cedarish %f > %o |> \$(ROOT)/image/${TARGET}-builder.json
RULES

# build the TARGET-compiled image from the TARGET-builder image which includes bindata.go
cat <<RULES
: img/compile.sh bin/go-bindata app/compiler $(cat img/compile-inputs.txt | tr '\n' ' ') | \$(ROOT)/<builder> \$(ROOT)/image/${TARGET}-builder.json |> ^ build-layer ${TARGET}-compiled^ \$(ROOT)/builder/bin/flynn-build layer --limits memory=2G ${TARGET}-builder %f > %o |> layer/compiled.json
: layer/compiled.json | \$(ROOT)/<builder> \$(ROOT)/util/cedarish/<image> |> ^ build-artifact ${TARGET}-builder^ \$(ROOT)/builder/bin/flynn-build artifact cedarish %f > %o |> \$(ROOT)/image/${TARGET}-compiled.json
RULES

# extract the bindata.go file from the compiled image
cat <<RULES
: \$(ROOT)/image/${TARGET}-compiled.json | \$(ROOT)/<builder> |> \$(ROOT)/host/bin/flynn-host run %f cat /bindata.go > %o |> bindata.go
RULES
