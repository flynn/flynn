package sum

import (
	"testing"

	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/docker/docker/pkg/tarsum"
)

func TestVersionCheck(t *testing.T) {
	tests := []struct {
		String   string
		Expected tarsum.Version
		Err      error
	}{
		{"farts", tarsum.Version(-1), tarsum.ErrVersionNotImplemented},
		{"1", tarsum.Version1, nil},
		{"Version1", tarsum.Version1, nil},
		{"Version0", tarsum.Version0, nil},
		{"0", tarsum.Version0, nil},
		{"dev", tarsum.VersionDev, nil},
	}

	for i, test := range tests {
		v, err := DetermineVersion(test.String)
		if err != test.Err {
			t.Errorf("%d: Expected error %q, got %q", i, test.Err, err)
		}
		if v != test.Expected {
			t.Errorf("%d: Expected version %q, got %q", i, test.Expected, v)
		}
	}
}
