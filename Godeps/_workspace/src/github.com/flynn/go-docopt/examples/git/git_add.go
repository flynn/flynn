package main

import (
	"fmt"
	"github.com/docopt/docopt-go"
)

func cmdAdd(argv []string) (err error) {
	usage := `usage: git add [options] [--] [<filepattern>...]

options:
    -h, --help
    -n, --dry-run        dry run
    -v, --verbose        be verbose
    -i, --interactive    interactive picking
    -p, --patch          select hunks interactively
    -e, --edit           edit current diff and apply
    -f, --force          allow adding otherwise ignored files
    -u, --update         update tracked files
    -N, --intent-to-add  record only the fact that the path will be added later
    -A, --all            add all, noticing removal of tracked files
    --refresh            don't add, only refresh the index
    --ignore-errors      just skip files which cannot be added because of errors
    --ignore-missing     check if - even missing - files are ignored in dry run
`

	args, _ := docopt.Parse(usage, nil, true, "", false)
	fmt.Println(args)
	return
}
