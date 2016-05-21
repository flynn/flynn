package sum

import "github.com/flynn/flynn/Godeps/_workspace/src/github.com/docker/docker/pkg/tarsum"

var (
	// mapping for flag parsing
	tarsumVersions = map[string]tarsum.Version{
		"Version0":   tarsum.Version0,
		"Version1":   tarsum.Version1,
		"VersionDev": tarsum.VersionDev,
		"0":          tarsum.Version0,
		"1":          tarsum.Version1,
		"dev":        tarsum.VersionDev,
	}
)

// DetermineVersion parses a human provided string (like a flag argument) and
// determines the tarsum.Version to return
func DetermineVersion(vstr string) (tarsum.Version, error) {
	for key, val := range tarsumVersions {
		if key == vstr {
			return val, nil
		}
	}
	return tarsum.Version(-1), tarsum.ErrVersionNotImplemented
}
