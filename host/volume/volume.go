package volume

import (
	"github.com/flynn/flynn/host/types"
)

/*
	A Volume is a persistent and sharable filesystem.  Unlike most of the filesystem in a job's
	container, which is ephemeral and is discarded after job termination, Volumes can be used to
	store data and may be reconnected to a later job (or to multiple jobs).

	Volumes may also support additional features for their section of the filesystem, such
	storage quotas, read-only mounts, snapshotting operation, etc.

	The Flynn host service maintains a locally persistent knowledge
	of mounts, and supplies this passively to the orchestration API.
	The host service does *not* perform services such as garbage collection of unmounted
	volumes (how is it to know whether you still want that data preserved for a future job?)
	or transport and persistence of volumes between hosts (that should be orchestrated via
	the API from a higher level service).
*/
type Volume interface {
	ID() string // guid (v4, random.  not globally sync'd, entropy should be high enough to be unique)

	Mounts() map[VolumeMount]struct{}

	// Note: NOT provided: a method that gets the host's path to a mount.  Not all backends have such a useable raw path on the host.

	Mount(job host.ActiveJob, path string) (VolumeMount, error)

	TakeSnapshot() (Volume, error)
}

/*
	VolumeMount names the location in which a shared+persistent filesystem is mounted into a job's container.

	A Volume has a one-to-many relationship with `VolumeMount`s -- the same volume
	may be mounted to many containers (or even multiple places within a single container).
*/
type VolumeMount struct {
	JobID    string // job which the volume is mounted to
	Location string // path within the container where the mount shall appear
}
