package cli

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"fmt"
	"time"

	host "github.com/flynn/flynn/host/types"
	"github.com/flynn/flynn/pkg/cluster"
	"github.com/flynn/go-docopt"
)

func init() {
	Register("cli-add-command", runCliAddCommand, `
usage: flynn-host cli-add-command

Get the 'flynn cluster add' command to manage this cluster.`)
}

func runCliAddCommand(args *docopt.Args, client *cluster.Client) error {
	hosts, err := client.Hosts()
	if err != nil {
		return fmt.Errorf("could not list hosts: %v", err)
	}
	if len(hosts) == 0 {
		return errors.New("no hosts found")
	}
	var (
		domain           string
		key              string
		pin              string
		dashboardAppName string
		dashboardDomain  string
		dashboardToken   string
	)
	controller := &mostRecentJob{app: "controller", typ: "web", check: func(job *host.Job) error {
		domain = job.Config.Env["DEFAULT_ROUTE_DOMAIN"]
		key = job.Config.Env["AUTH_KEY"]
		if domain == "" {
			return errors.New("cannot retrieve domain")
		}
		if key == "" {
			return errors.New("cannot retrieve controller auth key")
		}
		return nil
	}}
	router := &mostRecentJob{app: "router", typ: "app", check: func(job *host.Job) error {
		b, _ := pem.Decode([]byte(job.Config.Env["TLSCERT"]))
		sha := sha256.Sum256(b.Bytes)
		pin = base64.StdEncoding.EncodeToString(sha[:])
		if pin == "" {
			return errors.New("cannot retrieve TLS pin")
		}
		return nil
	}}
	dashboard := &mostRecentJob{app: "dashboard", typ: "web", check: func(job *host.Job) error {
		dashboardAppName = job.Config.Env["APP_NAME"]
		dashboardDomain = job.Config.Env["DEFAULT_ROUTE_DOMAIN"]
		dashboardToken = job.Config.Env["LOGIN_TOKEN"]
		if dashboardAppName == "" {
			return errors.New("cannot retrieve dashboard app name")
		}
		if dashboardDomain == "" {
			return errors.New("cannot retrieve dashboard domain")
		}
		if dashboardToken == "" {
			return errors.New("cannot retrieve dashboard login token")
		}
		return nil
	}}
	for _, h := range hosts {
		hostJobs, err := h.ListJobs()
		if err != nil {
			return fmt.Errorf("could not get jobs for host %v: %v", h.ID(), err)
		}
		for _, job := range hostJobs {
			p := &job
			controller.offer(p)
			router.offer(p)
			dashboard.offer(p)
		}
	}

	if err := controller.err(); err != nil {
		return err
	}
	if err := router.err(); err != nil {
		return err
	}
	if err := dashboard.err(); err != nil {
		return err
	}

	fmt.Printf("Install the Flynn CLI (see https://flynn.io/docs/cli for instructions) and paste the line below into a terminal window:\n\n")
	fmt.Printf("flynn cluster add -p %v default %v %v\n", pin, domain, key)
	fmt.Printf("\nThe built-in dashboard can be accessed at http://%v.%v and your login token is %v\n", dashboardAppName, dashboardDomain, dashboardToken)

	return nil
}

type mostRecentJob struct {
	app   string
	typ   string
	ts    time.Time
	check func(*host.Job) error
	job   *host.Job
}

func (j *mostRecentJob) offer(job *host.ActiveJob) {
	app := job.Job.Metadata["flynn-controller.app_name"]
	typ := job.Job.Metadata["flynn-controller.type"]
	if app != j.app || typ != j.typ {
		return
	}
	if job.CreatedAt.Unix() < j.ts.Unix() {
		return
	}
	j.job = job.Job
}

func (j *mostRecentJob) err() error {
	if j.job == nil {
		return fmt.Errorf("no job %v of type %v was found", j.app, j.typ)
	}
	return j.check(j.job)
}
