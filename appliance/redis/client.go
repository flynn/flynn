package redis

// Status represents response to the /status endpoint.
type Status struct {
	Process *ProcessInfo `json:"process"`
}
