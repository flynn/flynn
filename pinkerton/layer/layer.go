package layer

type PullInfo struct {
	Repo   string `json:"repo"`
	Type   Type   `json:"type"`
	ID     string `json:"id"`
	Status Status `json:"status"`
}

type Type string

const (
	TypeImage Type = "image"
	TypeLayer Type = "layer"
)

type Status string

const (
	StatusExists     Status = "exists"
	StatusDownloaded Status = "downloaded"
)
