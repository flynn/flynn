package version

import "fmt"

var commit, branch, tag, dirty string

func String() string {
	if commit == "" || commit == "dev" {
		return "dev"
	}
	if Tagged() {
		return tag
	}
	if dirty == "true" {
		commit += "+"
	}
	return fmt.Sprintf("%s (%s)", commit, branch)
}

func Tagged() bool {
	return tag != "none" && dirty == "false"
}
