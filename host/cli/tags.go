package cli

import (
	"fmt"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/flynn/flynn/pkg/cluster"
	"github.com/flynn/go-docopt"
)

func init() {
	Register("tags", runTags, `
usage: flynn-host tags
       flynn-host tags set <hostid> <var>=<val>...
       flynn-host tags del <hostid> <var>...

Manage flynn-host daemon tags.

Commands:
	With no arguments, shows a list of current tags.

	set    sets value of one or more tags
	del    deletes one or more tags
`)
}

func runTags(args *docopt.Args, client *cluster.Client) error {
	if args.Bool["set"] {
		return runTagsSet(args, client)
	} else if args.Bool["del"] {
		return runTagsDel(args, client)
	}
	hosts, err := client.Hosts()
	if err != nil {
		return err
	}
	w := tabwriter.NewWriter(os.Stdout, 1, 2, 2, ' ', 0)
	defer w.Flush()
	listRec(w, "HOST", "TAGS")
	for _, host := range hosts {
		tags := make([]string, 0, len(host.Tags()))
		for k, v := range host.Tags() {
			tags = append(tags, fmt.Sprintf("%s=%s", k, v))
		}
		listRec(w, host.ID(), strings.Join(tags, " "))
	}
	return nil
}

func runTagsSet(args *docopt.Args, client *cluster.Client) error {
	host, err := client.Host(args.String["<hostid>"])
	if err != nil {
		return err
	}
	pairs := args.All["<var>=<val>"].([]string)
	tags := make(map[string]string, len(pairs))
	for _, s := range pairs {
		keyVal := strings.SplitN(s, "=", 2)
		if len(keyVal) == 1 && keyVal[0] != "" {
			tags[keyVal[0]] = "true"
		} else if len(keyVal) == 2 {
			tags[keyVal[0]] = keyVal[1]
		}
	}
	return host.UpdateTags(tags)
}

func runTagsDel(args *docopt.Args, client *cluster.Client) error {
	host, err := client.Host(args.String["<hostid>"])
	if err != nil {
		return err
	}
	vars := args.All["<var>"].([]string)
	tags := make(map[string]string, len(vars))
	for _, v := range vars {
		// empty tags get deleted on the host
		tags[v] = ""
	}
	return host.UpdateTags(tags)
}
