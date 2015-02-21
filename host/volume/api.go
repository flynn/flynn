package volume

type PullCoordinate struct {
	HostID     string `json:"host_id"`
	SnapshotID string `json:"snapshot_id"`
}
