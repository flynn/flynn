package bootstrap

import (
	"fmt"
)

type LogAction struct {
	Output string `json:"output"`
}

func init() {
	Register("log", &LogAction{})
}

func (a *LogAction) Run(s *State) error {
	fmt.Println(interpolate(s, a.Output))
	return nil
}
