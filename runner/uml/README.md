# User-mode Linux

This directory contains a Makefile and kernel config that builds a User-mode
Linux binary patched to include AUFS. See `../rootfs` for information on
building and running VMs using this binary.

## Building

```go
make
```

Requires `build-essential` and `xz-utils`.
