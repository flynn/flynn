package main

import (
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"

	"github.com/flynn/flynn-host/types"
	"github.com/flynn/go-discoverd"
	"github.com/flynn/go-dockerclient"
	"github.com/flynn/go-flynn/cluster"
	strowgerc "github.com/flynn/strowger/client"
	"github.com/flynn/strowger/types"
	"github.com/titanous/go-tigertonic"
)

func main() {
	var err error
	clusterc, err = cluster.NewClient()
	if err != nil {
		log.Fatal(err)
	}
	router, err = strowgerc.New()
	if err != nil {
		log.Fatal(err)
	}

	mux := tigertonic.NewTrieServeMux()
	mux.Handle("PUT", "/apps/{app_id}/domains/{domain}", tigertonic.Marshaled(addDomain))
	mux.Handle("POST", "/apps/{app_id}/formation/{formation_id}", tigertonic.Marshaled(changeFormation))
	mux.Handle("GET", "/apps/{app_id}/jobs", tigertonic.Marshaled(getJobs))
	mux.HandleFunc("GET", "/apps/{app_id}/jobs/{job_id}/logs", getJobLog)
	mux.HandleFunc("POST", "/apps/{app_id}/jobs", runJob)
	http.ListenAndServe("127.0.0.1:1200", tigertonic.Logged(mux, nil))
}

var clusterc *cluster.Client
var router *strowgerc.Client

type Job struct {
	ID   string `json:"id"`
	Type string `json:"type"`
}

func addDomain(u *url.URL, h http.Header, data *struct{}) (int, http.Header, struct{}, error) {
	q := u.Query()
	if err := router.AddFrontend(&strowger.Config{Service: q.Get("app_id"), HTTPDomain: q.Get("domain")}); err != nil {
		return 500, nil, struct{}{}, err
	}
	return 200, nil, struct{}{}, nil
}

// GET /apps/{app_id}/jobs
func getJobs(u *url.URL, h http.Header) (int, http.Header, []Job, error) {
	hosts, err := clusterc.ListHosts()
	if err != nil {
		return 500, nil, nil, err
	}

	q := u.Query()
	prefix := q.Get("app_id") + "-"
	jobs := make([]Job, 0)
	for _, host := range hosts {
		for _, job := range host.Jobs {
			if strings.HasPrefix(job.ID, prefix) {
				typ := strings.Split(job.ID[len(prefix):], ".")[0]
				jobs = append(jobs, Job{ID: job.ID, Type: typ})
			}
		}
	}

	return 200, nil, jobs, nil
}

type Formation struct {
	Quantity int    `json:"quantity"`
	Type     string `json:"type"`
}

func shelfURL() string {
	services, _ := discoverd.Services("shelf", discoverd.DefaultTimeout)
	if len(services) < 1 {
		panic("Shelf is not discoverable")
	}
	return services[0].Addr
}

// POST /apps/{app_id}/formation/{formation_id}
func changeFormation(u *url.URL, h http.Header, req *Formation) (int, http.Header, *Formation, error) {
	hosts, err := clusterc.ListHosts()
	if err != nil {
		log.Println("scheduler state error", err)
		return 500, nil, nil, err
	}

	q := u.Query()
	req.Type = q.Get("formation_id")
	prefix := q.Get("app_id") + "-" + req.Type + "."
	var jobs []*host.Job
	for _, h := range hosts {
		for _, job := range h.Jobs {
			if strings.HasPrefix(job.ID, prefix) {
				if job.Attributes == nil {
					job.Attributes = make(map[string]string)
				}
				job.Attributes["host_id"] = h.ID
				jobs = append(jobs, job)
			}
		}
	}

	if req.Quantity < 0 {
		req.Quantity = 0
	}
	diff := req.Quantity - len(jobs)
	log.Printf("have %d %s, diff %d", len(jobs), req.Type, diff)
	if diff > 0 {
		config := &docker.Config{
			Image:        "flynn/slugrunner",
			Cmd:          []string{"start", req.Type},
			AttachStdout: true,
			AttachStderr: true,
			Env:          []string{"SLUG_URL=http://" + shelfURL() + "/" + q.Get("app_id") + ".tgz"},
		}
		if req.Type == "web" {
			config.Env = append(config.Env, "SD_NAME="+q.Get("app_id"))
		}
		addReq := &host.AddJobsReq{
			HostJobs: make(map[string][]*host.Job),
		}
	outer:
		for {
			for h := range hosts {
				addReq.HostJobs[h] = append(addReq.HostJobs[h], &host.Job{ID: cluster.RandomJobID(prefix), TCPPorts: 1, Config: config})
				diff--
				if diff == 0 {
					break outer
				}
			}
		}

		res, err := clusterc.AddJobs(addReq)
		if err != nil || !res.Success {
			log.Println("schedule error", err)
			return 500, nil, nil, err
		}
	} else if diff < 0 {
		for _, job := range jobs[:-diff] {
			host, err := clusterc.ConnectHost(job.Attributes["host_id"])
			if err != nil {
				log.Println("error connecting to", job.Attributes["host_id"], err)
				continue
			}
			if err := host.StopJob(job.ID); err != nil {
				log.Println("error stopping", job.ID, "on", job.Attributes["host_id"], err)
			}
		}
	}

	return 200, nil, req, nil
}

// GET /apps/{app_id}/jobs/{job_id}/logs
func getJobLog(w http.ResponseWriter, req *http.Request) {
	hosts, err := clusterc.ListHosts()
	if err != nil {
		w.WriteHeader(500)
		log.Println(err)
		return
	}

	q := req.URL.Query()
	jobID := q.Get("job_id")
	if prefix := q.Get("app_id") + "-"; !strings.HasPrefix(jobID, prefix) {
		jobID = prefix + jobID
	}
	var job *host.Job
	var h host.Host
outer:
	for _, h = range hosts {
		for _, job = range h.Jobs {
			if job.ID == jobID {
				break outer
			}
		}
		job = nil
	}
	if job == nil {
		w.WriteHeader(404)
		return
	}

	attachReq := &host.AttachReq{
		JobID: job.ID,
		Flags: host.AttachFlagStdout | host.AttachFlagStderr | host.AttachFlagLogs,
	}

	client, err := clusterc.ConnectHost(h.ID)
	if err != nil {
		w.WriteHeader(500)
		log.Println("lorne connect failed", err)
		return
	}
	attachConn, _, err := client.Attach(attachReq, false)
	if err != nil {
		w.WriteHeader(500)
		log.Println("attach failed", err)
		return
	}
	defer attachConn.Close()
	io.Copy(w, attachConn)
}

type NewJob struct {
	Cmd     []string          `json:"cmd"`
	Env     map[string]string `json:"env"`
	Attach  bool              `json:"attach"`
	TTY     bool              `json:"tty"`
	Columns int               `json:"tty_columns"`
	Lines   int               `json:"tty_lines"`
}

// POST /apps/{app_id}/jobs
func runJob(w http.ResponseWriter, req *http.Request) {
	var jobReq NewJob
	if err := json.NewDecoder(req.Body).Decode(&jobReq); err != nil {
		w.WriteHeader(500)
		log.Println(err)
		return
	}

	hosts, err := clusterc.ListHosts()
	if err != nil {
		w.WriteHeader(500)
		log.Println(err)
		return
	}
	// pick a random host
	var hostID string
	for hostID = range hosts {
		break
	}
	if hostID == "" {
		w.WriteHeader(500)
		log.Println("no hosts found")
		return
	}

	env := make([]string, 0, len(jobReq.Env))
	for k, v := range jobReq.Env {
		env = append(env, k+"="+v)
	}

	q := req.URL.Query()
	job := &host.Job{
		ID: cluster.RandomJobID(q.Get("app_id") + "-run."),
		Config: &docker.Config{
			Image:        "flynn/slugrunner",
			Cmd:          jobReq.Cmd,
			AttachStdin:  true,
			AttachStdout: true,
			AttachStderr: true,
			StdinOnce:    true,
			Env:          append(env, "SLUG_URL=http://"+shelfURL()+"/"+q.Get("app_id")+".tgz"),
		},
	}
	if jobReq.TTY {
		job.Config.Tty = true
	}
	if jobReq.Attach {
		job.Config.AttachStdin = true
		job.Config.StdinOnce = true
		job.Config.OpenStdin = true
	}

	var attachConn cluster.ReadWriteCloser
	var attachWait func() error
	if jobReq.Attach {
		attachReq := &host.AttachReq{
			JobID:  job.ID,
			Flags:  host.AttachFlagStdout | host.AttachFlagStderr | host.AttachFlagStdin | host.AttachFlagStream,
			Height: jobReq.Lines,
			Width:  jobReq.Columns,
		}
		client, err := clusterc.ConnectHost(hostID)
		if err != nil {
			w.WriteHeader(500)
			log.Println("lorne connect failed", err)
			return
		}
		attachConn, attachWait, err = client.Attach(attachReq, true)
		if err != nil {
			w.WriteHeader(500)
			log.Println("attach failed", err)
			return
		}
		defer attachConn.Close()
	}

	res, err := clusterc.AddJobs(&host.AddJobsReq{HostJobs: map[string][]*host.Job{hostID: {job}}})
	if err != nil || !res.Success {
		w.WriteHeader(500)
		log.Println("schedule failed", err)
		return
	}

	if jobReq.Attach {
		if err := attachWait(); err != nil {
			w.WriteHeader(500)
			log.Println("attach wait failed", err)
			return
		}
		w.Header().Set("Content-Type", "application/vnd.flynn.hijack")
		w.Header().Set("Content-Length", "0")
		w.WriteHeader(200)
		conn, _, err := w.(http.Hijacker).Hijack()
		if err != nil {
			return
		}
		defer conn.Close()

		done := make(chan struct{})
		copy := func(to cluster.ReadWriteCloser, from io.Reader) {
			io.Copy(to, from)
			to.CloseWrite()
			done <- struct{}{}
		}
		go copy(conn.(cluster.ReadWriteCloser), attachConn)
		go copy(attachConn, conn)
		<-done
		<-done

		return
	}
	w.WriteHeader(200)
}
