// +build linux

package pinkerton

import (
	_ "github.com/flynn/flynn/Godeps/_workspace/src/github.com/docker/docker/daemon/graphdriver/aufs"
	_ "github.com/flynn/flynn/Godeps/_workspace/src/github.com/docker/docker/daemon/graphdriver/btrfs"
	_ "github.com/flynn/flynn/Godeps/_workspace/src/github.com/docker/docker/daemon/graphdriver/devmapper"
	_ "github.com/flynn/flynn/Godeps/_workspace/src/github.com/docker/docker/daemon/graphdriver/vfs"
)
