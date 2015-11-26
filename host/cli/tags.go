package cli

import (
	"fmt"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/flynn/go-docopt"
	"github.com/flynn/flynn/host/types"
	"github.com/flynn/flynn/pkg/cluster"
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
	instances, err := client.HostInstances()
	if err != nil {
		return err
	}
	w := tabwriter.NewWriter(os.Stdout, 1, 2, 2, ' ', 0)
	defer w.Flush()
	listRec(w, "HOST", "TAGS")
	for _, inst := range instances {
		tags := make([]string, 0, len(inst.Meta))
		for k, v := range inst.Meta {
			if strings.HasPrefix(k, host.TagPrefix) {
				tags = append(tags, fmt.Sprintf("%s=%s", strings.TrimPrefix(k, host.TagPrefix), v))
			}
		}
		listRec(w, inst.Meta["id"], strings.Join(tags, " "))
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
