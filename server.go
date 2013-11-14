package main

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"encoding/gob"
	"encoding/json"
	"errors"
	"io"
	"io/ioutil"
	"log"
	"net"
	"net/http"
	"net/url"
	"strings"

	"github.com/flynn/go-discover/discover"
	lornec "github.com/flynn/lorne/client"
	"github.com/flynn/lorne/types"
	sampic "github.com/flynn/sampi/client"
	"github.com/flynn/sampi/types"
	strowgerc "github.com/flynn/strowger/client"
	"github.com/flynn/strowger/types"
	"github.com/titanous/go-dockerclient"
	"github.com/titanous/go-tigertonic"
)

func main() {
	var err error
	scheduler, err = sampic.New()
	if err != nil {
		log.Fatal(err)
	}
	disc, err = discover.NewClient()
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

var scheduler *sampic.Client
var disc *discover.Client
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
	state, err := scheduler.State()
	if err != nil {
		return 500, nil, nil, err
	}

	q := u.Query()
	prefix := q.Get("app_id") + "-"
	jobs := make([]Job, 0)
	for _, host := range state {
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
	set, _ := disc.Services("shelf")
	addrs := set.OnlineAddrs()
	if len(addrs) < 1 {
		panic("Shelf is not discoverable")
	}
	return addrs[0]
}

// POST /apps/{app_id}/formation/{formation_id}
func changeFormation(u *url.URL, h http.Header, req *Formation) (int, http.Header, *Formation, error) {
	state, err := scheduler.State()
	if err != nil {
		log.Println("scheduler state error", err)
		return 500, nil, nil, err
	}

	q := u.Query()
	req.Type = q.Get("formation_id")
	prefix := q.Get("app_id") + "-" + req.Type + "."
	var jobs []*sampi.Job
	for _, host := range state {
		for _, job := range host.Jobs {
			if strings.HasPrefix(job.ID, prefix) {
				if job.Attributes == nil {
					job.Attributes = make(map[string]string)
				}
				job.Attributes["host_id"] = host.ID
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
		schedReq := &sampi.ScheduleReq{
			HostJobs: make(map[string][]*sampi.Job),
		}
	outer:
		for {
			for host := range state {
				schedReq.HostJobs[host] = append(schedReq.HostJobs[host], &sampi.Job{ID: prefix + randomID(), TCPPorts: 1, Config: config})
				diff--
				if diff == 0 {
					break outer
				}
			}
		}

		res, err := scheduler.Schedule(schedReq)
		if err != nil || !res.Success {
			log.Println("schedule error", err)
			return 500, nil, nil, err
		}
	} else if diff < 0 {
		for _, job := range jobs[:-diff] {
			host, err := lornec.New(job.Attributes["host_id"])
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
	state, err := scheduler.State()
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
	var job *sampi.Job
	var host sampi.Host
outer:
	for _, host = range state {
		for _, job = range host.Jobs {
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

	attachReq := &lorne.AttachReq{
		JobID: job.ID,
		Flags: lorne.AttachFlagStdout | lorne.AttachFlagStderr | lorne.AttachFlagLogs,
	}
	err, errChan := lorneAttach(host.ID, attachReq, w, nil)
	if err != nil {
		w.WriteHeader(500)
		log.Println("attach error", err)
		return
	}
	if err := <-errChan; err != nil {
		log.Println("attach failed", err)
	}
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

	state, err := scheduler.State()
	if err != nil {
		w.WriteHeader(500)
		log.Println(err)
		return
	}
	// pick a random host
	var hostID string
	for hostID = range state {
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
	job := &sampi.Job{
		ID: q.Get("app_id") + "-run." + randomID(),
		Config: &docker.Config{
			Image:        "ubuntu",
			Cmd:          jobReq.Cmd,
			AttachStdin:  true,
			AttachStdout: true,
			AttachStderr: true,
			StdinOnce:    true,
			Env:          env,
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

	outR, outW := io.Pipe()
	inR, inW := io.Pipe()
	defer outR.Close()
	defer inW.Close()
	var errChan <-chan error
	if jobReq.Attach {
		attachReq := &lorne.AttachReq{
			JobID:  job.ID,
			Flags:  lorne.AttachFlagStdout | lorne.AttachFlagStderr | lorne.AttachFlagStdin | lorne.AttachFlagStream,
			Height: jobReq.Lines,
			Width:  jobReq.Columns,
		}
		err, errChan = lorneAttach(hostID, attachReq, outW, inR)
		if err != nil {
			w.WriteHeader(500)
			log.Println("attach failed", err)
			return
		}
	}

	res, err := scheduler.Schedule(&sampi.ScheduleReq{HostJobs: map[string][]*sampi.Job{hostID: {job}}})
	if err != nil || !res.Success {
		w.WriteHeader(500)
		log.Println("schedule failed", err)
		return
	}

	if jobReq.Attach {
		w.Header().Set("Content-Type", "application/vnd.flynn.hijack")
		w.Header().Set("Content-Length", "0")
		w.WriteHeader(200)
		conn, bufrw, err := w.(http.Hijacker).Hijack()
		if err != nil {
			return
		}
		bufrw.Flush()
		go func() {
			buf := make([]byte, bufrw.Reader.Buffered())
			bufrw.Read(buf)
			inW.Write(buf)
			io.Copy(inW, conn)
			inW.Close()
		}()
		go io.Copy(conn, outR)
		<-errChan
		conn.Close()
		return
	}
	w.WriteHeader(200)
}

func lorneAttach(host string, req *lorne.AttachReq, out io.Writer, in io.Reader) (error, <-chan error) {
	services, err := disc.Services("flynn-lorne-attach." + host)
	if err != nil {
		return err, nil
	}
	addrs := services.OnlineAddrs()
	if len(addrs) == 0 {
		return err, nil
	}
	conn, err := net.Dial("tcp", addrs[0])
	if err != nil {
		return err, nil
	}
	err = gob.NewEncoder(conn).Encode(req)
	if err != nil {
		conn.Close()
		return err, nil
	}

	errChan := make(chan error)

	attach := func() {
		defer conn.Close()
		inErr := make(chan error, 1)
		if in != nil {
			go func() {
				io.Copy(conn, in)
			}()
		} else {
			close(inErr)
		}
		_, outErr := io.Copy(out, conn)
		if outErr != nil {
			errChan <- outErr
			return
		}
		errChan <- <-inErr
	}

	attachState := make([]byte, 1)
	if _, err := conn.Read(attachState); err != nil {
		conn.Close()
		return err, nil
	}
	switch attachState[0] {
	case lorne.AttachError:
		errBytes, err := ioutil.ReadAll(conn)
		conn.Close()
		if err != nil {
			return err, nil
		}
		return errors.New(string(errBytes)), nil
	case lorne.AttachWaiting:
		go func() {
			if _, err := conn.Read(attachState); err != nil {
				conn.Close()
				errChan <- err
				return
			}
			if attachState[0] == lorne.AttachError {
				errBytes, err := ioutil.ReadAll(conn)
				conn.Close()
				if err != nil {
					errChan <- err
					return
				}
				errChan <- errors.New(string(errBytes))
				return
			}
			attach()
		}()
		return nil, errChan
	default:
		go attach()
		return nil, errChan
	}
}

func randomID() string {
	b := make([]byte, 16)
	enc := make([]byte, 24)
	_, err := io.ReadFull(rand.Reader, b)
	if err != nil {
		panic(err) // This shouldn't ever happen, right?
	}
	base64.URLEncoding.Encode(enc, b)
	return string(bytes.TrimRight(enc, "="))
}
