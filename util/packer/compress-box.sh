#!/bin/bash

set -e -x

mkdir box
tar -xvzC box -f "$1"

# round-trip through vdi format to get rid of deflate compression in vmdk
for f in box/*.vmdk; do
  VBoxManage clonehd --format vdi $f $f.vdi
  VBoxManage closemedium disk --delete $f
  VBoxManage modifyhd --compact $f.vdi
  VBoxManage clonehd --format vmdk $f.vdi $f
  VBoxManage closemedium disk --delete $f.vdi
  VBoxManage closemedium disk $f
done

tar -cC box $(ls box) | pxz -ce - > "$1.xz"

rm -rf box
