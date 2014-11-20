package bootstrap

import (
	"fmt"
	"os"
	"strings"
)

type RequireEnv struct {
	Vars []string `json:"vars"`
}

func init() {
	Register("require-env", &RequireEnv{})
}

func (a *RequireEnv) Run(s *State) error {
	missing := make([]string, 0, len(a.Vars))
	for _, v := range a.Vars {
		if os.Getenv(v) == "" {
			missing = append(missing, v)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("Missing required environment variable(s): %s", strings.Join(missing, ", "))
	}
	return nil
}
