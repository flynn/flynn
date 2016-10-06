package version

import (
	"fmt"
	"strconv"
)

var commit, branch, tag, dirty string

func String() string {
	if Dev() {
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

func Dev() bool {
	return commit == "" || commit == "dev"
}

func Tagged() bool {
	return tag != "none" && dirty == "false"
}

type Version struct {
	Dev       bool
	Date      string
	Iteration int
}

func (v *Version) Before(other *Version) bool {
	return v.Date < other.Date || v.Date == other.Date && v.Iteration < other.Iteration
}

func Parse(s string) *Version {
	if len(s) == 0 || s[0] != 'v' || len(s) < 11 {
		return &Version{Dev: true}
	}
	v := &Version{Date: s[1:9]}
	v.Iteration, _ = strconv.Atoi(s[10:])
	return v
}
