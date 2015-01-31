package layer

type PullInfo struct {
	Repo   string `json:"repo"`
	ID     string `json:"id"`
	Status Status `json:"status"`
}

type Status string

const (
	StatusExists     Status = "exists"
	StatusDownloaded Status = "downloaded"
)
