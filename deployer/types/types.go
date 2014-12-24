package deployer

import "time"

type Deployment struct {
	ID           string          `json:"id,omitempty"`
	AppID        string          `json:"app_id,omitempty"`
	OldReleaseID string          `json:"old_release_id,omitempty"`
	NewReleaseID string          `json:"new_release_id,omitempty"`
	Strategy     string          `json:"strategy,omitempty"`
	CreatedAt    *time.Time      `json:"created_at,omitempty"`
	Steps        map[string]Step `json:"steps,omitempty"`
	Status       Status          `json:"status,omitempty"`
}

type Status uint8

const (
	StatusWaiting Status = iota
	StatusRunning
	StatusDone
	StatusCrashed
	StatusFailed
)

type Step struct {
	Cmd []string `json:"cmd,omitempty"`
}

type DeploymentEvent struct {
	ID           int64      `json:"id"`
	DeploymentID string     `json:"deployment_id"`
	ReleaseID    string     `json:"release_id"`
	JobType      string     `json:"job_type"`
	JobState     string     `json:"job_state"`
	CreatedAt    *time.Time `json:"created_at"`
}
