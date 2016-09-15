package main

import (
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"time"

	"github.com/flynn/flynn/pkg/version"
)

var telemetryURL = "https://dl.flynn.io/measure/scheduler"

func init() {
	if u := os.Getenv("TELEMETRY_URL"); u != "" {
		telemetryURL = u
	}
}

func (s *Scheduler) tickSendTelemetry() {
	go func() {
		for range time.Tick(12 * time.Hour) {
			s.triggerSendTelemetry()
		}
	}()
}

func (s *Scheduler) triggerSendTelemetry() {
	select {
	case s.sendTelemetry <- struct{}{}:
	default:
	}
}

func (s *Scheduler) SendTelemetry() {
	if !s.IsLeader() || os.Getenv("TELEMETRY_DISABLED") == "true" {
		return
	}

	params := make(url.Values)
	params.Add("id_version", version.String())
	params.Add("id_bootstrap", os.Getenv("TELEMETRY_BOOTSTRAP_ID"))
	params.Add("id_cluster", os.Getenv("TELEMETRY_CLUSTER_ID"))

	params.Add("ct_hosts", strconv.Itoa(len(s.hosts)))

	var jobs int
	for _, j := range s.jobs {
		if j.State == JobStateRunning {
			jobs++
		}
	}
	params.Add("ct_running_jobs", strconv.Itoa(jobs))

	var formations int
	apps := make(map[string]struct{})
	dbs := make(map[string]map[string]struct{})
	providers := []string{"postgres", "mongodb", "mysql", "redis"}
	for _, p := range providers {
		dbs[p] = make(map[string]struct{})
	}
	for _, f := range s.formations {
		if f.App.Meta["flynn-system-app"] == "true" || f.GetProcesses().IsEmpty() {
			continue
		}
		formations++
		apps[f.App.ID] = struct{}{}

		for _, p := range providers {
			switch p {
			case "postgres":
				if _, ok := f.Release.Env["FLYNN_POSTGRES"]; !ok {
					continue
				}
				if db := f.Release.Env["PGDATABASE"]; db != "" {
					dbs[p][db] = struct{}{}
				}
			case "mongodb":
				if _, ok := f.Release.Env["FLYNN_MONGO"]; !ok {
					continue
				}
				if db := f.Release.Env["MONGO_DATABASE"]; db != "" {
					dbs[p][db] = struct{}{}
				}
			case "mysql":
				if _, ok := f.Release.Env["FLYNN_MYSQL"]; !ok {
					continue
				}
				if db := f.Release.Env["MYSQL_DATABASE"]; db != "" {
					dbs[p][db] = struct{}{}
				}
			case "redis":
				if db := f.Release.Env["FLYNN_REDIS"]; db != "" {
					dbs[p][db] = struct{}{}
				}
			}
		}
	}
	params.Add("ct_running_apps", strconv.Itoa(len(apps)))
	params.Add("ct_running_formations", strconv.Itoa(formations))
	for _, p := range providers {
		params.Add(fmt.Sprintf("ct_%s_dbs", p), strconv.Itoa(len(dbs[p])))
	}

	go func() {
		req, _ := http.NewRequest("GET", telemetryURL, nil)
		req.Header.Set("User-Agent", "flynn-scheduler/"+version.String())
		req.URL.RawQuery = params.Encode()

		for i := 0; i < 5; i++ {
			res, err := http.DefaultClient.Do(req)
			if res != nil {
				res.Body.Close()
			}
			if err == nil && res.StatusCode == 200 {
				return
			}
			time.Sleep(10 * time.Second)
		}
	}()
}
