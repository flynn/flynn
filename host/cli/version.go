package cli

import (
	"fmt"

	"github.com/flynn/flynn/pkg/version"
)

func init() {
	Register("version", runVersion, `
usage: flynn-host version

Show current version`)
}

func runVersion() {
	fmt.Println(version.String())
}
