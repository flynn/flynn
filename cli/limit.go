package main

import (
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/flynn/go-docopt"
	"github.com/flynn/flynn/controller/client"
	ct "github.com/flynn/flynn/controller/types"
	"github.com/flynn/flynn/host/resource"
	"github.com/flynn/flynn/pkg/typeconv"
)

func init() {
	register("limit", runLimit, `
usage: flynn limit [-t <proc>]
       flynn limit set <proc> <var>=<val>...

Manage app resource limits.

Options:
	-t, --process-type=<proc>  set or read limits for specified process type

Commands:
	With no arguments, shows a list of resource limits.

	set    sets value of one or more resource limits

Examples:

	$ flynn limit
	web:     max_fd=10000  memory=1GB
	worker:  max_fd=10000  memory=1GB

	$ flynn limit set web memory=512MB max_fd=12000
	Created release 5058ae7964f74c399a240bdd6e7d1bcb

	$ flynn limit
	web:     max_fd=12000  memory=512MB
	worker:  max_fd=10000  memory=1GB

	$ flynn limit set web memory=256MB
	Created release b39fe25d0ea344b6b2af5cf4d6542a80

	$ flynn limit
	web:     max_fd=12000  memory=256MB
	worker:  max_fd=10000  memory=1GB
`)
}

func runLimit(args *docopt.Args, client *controller.Client) error {
	if args.Bool["set"] {
		return runLimitSet(args, client)
	}

	release, err := client.GetAppRelease(mustApp())
	if err == controller.ErrNotFound {
		return nil
	} else if err != nil {
		return err
	}

	w := tabWriter()
	defer w.Flush()

	if procType := args.String["--process-type"]; procType != "" {
		t, ok := release.Processes[procType]
		if !ok {
			return fmt.Errorf("unknown process type %q", procType)
		}
		formatLimits(w, procType, t.Resources)
		return nil
	}

	for s, t := range release.Processes {
		formatLimits(w, s, t.Resources)
	}
	return nil
}

func formatLimits(w io.Writer, s string, r resource.Resources) {
	limits := make([]string, 0, len(r))
	for typ, spec := range r {
		if limit := spec.Limit; limit != nil {
			limits = append(limits, fmt.Sprintf("%s=%s", typ, resource.FormatLimit(typ, *limit)))
		}
	}
	sort.Strings(limits)
	fmt.Fprintf(w, "%s:\t%s\n", s, strings.Join(limits, "\t"))
}

func runLimitSet(args *docopt.Args, client *controller.Client) error {
	proc := args.String["<proc>"]
	release, err := client.GetAppRelease(mustApp())
	if err == controller.ErrNotFound {
		release = &ct.Release{}
		if proc != "" {
			release.Processes = make(map[string]ct.ProcessType)
			release.Processes[proc] = ct.ProcessType{}
		}
	} else if err != nil {
		return err
	}

	t, ok := release.Processes[proc]
	if !ok {
		return fmt.Errorf("unknown process type %q", proc)
	}
	if t.Resources == nil {
		t.Resources = resource.Defaults()
	}

	limits := args.All["<var>=<val>"].([]string)
	for _, limit := range limits {
		typVal := strings.SplitN(limit, "=", 2)
		if len(typVal) != 2 {
			return fmt.Errorf("invalid resource limit: %q", limit)
		}
		typ, ok := resource.ToType(typVal[0])
		if !ok {
			return fmt.Errorf("invalid resource limit type: %q", typVal)
		}
		val, err := resource.ParseLimit(typ, typVal[1])
		if err != nil {
			return fmt.Errorf("invalid resource limit value: %q", typVal[1])
		}
		t.Resources[typ] = resource.Spec{Limit: typeconv.Int64Ptr(val)}
	}
	release.Processes[proc] = t

	release.ID = ""
	if err := client.CreateRelease(release); err != nil {
		return err
	}
	if err := client.DeployAppRelease(mustApp(), release.ID, nil); err != nil {
		return err
	}
	fmt.Printf("Created release %s\n", release.ID)
	return nil
}
