package volume

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
	Info() *Info

	Provider() Provider

	// Location returns the path to this volume's mount.  To use the volume in a job, bind mount this into the container's filesystem.
	Location() string

	TakeSnapshot() (Volume, error)
}

/*
	`volume.Info` names and describes info about a volume.
	It is a serializable structure intended for API use.
*/
type Info struct {
	// Volumes have a unique identifier.
	// These are guid formatted (v4, random); selected by the server;
	// and though not globally sync'd, entropy should be high enough to be unique.
	ID string `json:"id"`
}
