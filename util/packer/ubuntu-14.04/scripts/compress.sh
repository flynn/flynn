#!/bin/bash
set -xeo pipefail

# Zero out the free space to save space in the final image:
echo "Zeroing device to make space..."
dd if=/dev/zero of=/EMPTY bs=1M || true
rm -f /EMPTY
