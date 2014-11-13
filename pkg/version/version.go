package version

import "fmt"

var commit, branch, tag, dirty string

func String() string {
	if commit == "" {
		return "dev"
	}
	if tag != "none" && dirty == "false" {
		return tag
	}
	if dirty == "true" {
		commit += "+"
	}
	return fmt.Sprintf("%s (%s)", commit, branch)
}
