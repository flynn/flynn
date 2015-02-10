package sum

import (
	"testing"

	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/docker/docker/pkg/tarsum"
)

func TestChecks(t *testing.T) {
	checks := Checks{
		Check{Id: "120e218dd395ec314e7b6249f39d2853911b3d6def6ea164ae05722649f34b16", Source: "./busybox.tar", Hash: "7b0ade22d5bba35d1e88389c005376f441e7d83bf5f363f2d7c70be9286163aa", Version: tarsum.Version0},
	}

	if v := checks.Versions(); len(v) != 1 {
		t.Errorf("expected length %d, got %d", 1, len(v))
	}
}
