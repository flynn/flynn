package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/cupcake/goamz/aws"
	"github.com/cupcake/goamz/s3"
	"github.com/flynn/flynn-test/cluster"
	"github.com/gorilla/handlers"
)

var logBucket = "flynn-ci-logs"

type Runner struct {
	bc          cluster.BootConfig
	events      chan Event
	dockerFS    string
	githubToken string
	s3Bucket    *s3.Bucket
	networks    map[string]struct{}
	mtx         sync.Mutex
	builds      int
}

func NewRunner(bc cluster.BootConfig, dockerFS string) *Runner {
	return &Runner{
		bc:       bc,
		events:   make(chan Event, 10),
		dockerFS: dockerFS,
		networks: make(map[string]struct{}),
	}
}

func (r *Runner) start() error {
	r.githubToken = os.Getenv("GITHUB_TOKEN")
	if r.githubToken == "" {
		return errors.New("GITHUB_TOKEN not set")
	}

	awsAuth := aws.Auth{
		AccessKey: os.Getenv("AWS_ACCESS_KEY_ID"),
		SecretKey: os.Getenv("AWS_SECRET_ACCESS_KEY"),
	}
	if awsAuth.AccessKey == "" || awsAuth.SecretKey == "" {
		return errors.New("AWS_ACCESS_KEY_ID and AWS_SECRET_ACCESS_KEY not set")
	}
	r.s3Bucket = s3.New(awsAuth, aws.USEast).Bucket(logBucket)

	if r.dockerFS == "" {
		var err error
		bc := r.bc
		bc.Network, err = r.allocateNet()
		if err != nil {
			return err
		}
		if r.dockerFS, err = cluster.BuildFlynn(bc, "", repos, os.Stdout); err != nil {
			return fmt.Errorf("could not build flynn: %s", err)
		}
		r.releaseNet(bc.Network)
		defer os.RemoveAll(r.dockerFS)
	}

	go r.watchEvents()

	http.Handle("/", handlers.CombinedLoggingHandler(os.Stdout, http.HandlerFunc(r.httpEventHandler)))
	fmt.Println("Listening on :80...")
	if err := http.ListenAndServe(":80", nil); err != nil {
		return fmt.Errorf("ListenAndServer: %s", err)
	}
	return nil
}

func (r *Runner) watchEvents() {
	for event := range r.events {
		if !needsBuild(event) {
			continue
		}
		go func() {
			id := r.nextBuildId()
			if err := r.build(id, event.Repo(), event.Commit()); err != nil {
				fmt.Printf("build %d failed: %s\n", id, err)
				return
			}
			fmt.Printf("build %d passed!\n", id)
		}()
	}
}

func (r *Runner) nextBuildId() int {
	r.mtx.Lock()
	defer r.mtx.Unlock()
	r.builds++
	return r.builds
}

func (r *Runner) build(id int, repo, commit string) (err error) {
	r.updateStatus(repo, commit, "pending", "")

	var log bytes.Buffer
	defer func() {
		if err != nil {
			log.WriteString(fmt.Sprintf("build error: %s\n", err))
		}
		url := r.uploadToS3(log, id, repo, commit)
		if err == nil {
			r.updateStatus(repo, commit, "success", url)
		} else {
			r.updateStatus(repo, commit, "failure", url)
		}
	}()

	fmt.Printf("building %s[%s]\n", repo, commit)

	out := io.MultiWriter(os.Stdout, &log)
	repos := map[string]string{repo: commit}
	bc := r.bc
	bc.Network, err = r.allocateNet()
	if err != nil {
		return err
	}
	defer r.releaseNet(bc.Network)
	newDockerfs, err := cluster.BuildFlynn(bc, r.dockerFS, repos, out)
	defer os.RemoveAll(newDockerfs)
	if err != nil {
		msg := fmt.Sprintf("could not build flynn: %s\n", err)
		log.WriteString(msg)
		return errors.New(msg)
	}

	cmd := exec.Command(
		os.Args[0],
		"--user", r.bc.User,
		"--rootfs", r.bc.RootFS,
		"--dockerfs", newDockerfs,
		"--kernel", r.bc.Kernel,
		"--cli", *flagCLI,
		"--network", bc.Network,
		"--nat", r.bc.NatIface,
		"--debug",
	)
	cmd.Stdout = out
	cmd.Stderr = out
	return cmd.Run()
}

func (r *Runner) uploadToS3(log bytes.Buffer, id int, repo, commit string) string {
	name := fmt.Sprintf("%s-build%d-%s-%s.txt", repo, id, commit, time.Now().Format("2006-01-02-15-04-05"))
	url := fmt.Sprintf("https://s3.amazonaws.com/%s/%s", logBucket, name)
	fmt.Printf("uploading build log to S3: %s\n", url)
	if err := r.s3Bucket.Put(name, log.Bytes(), "text/plain", "public-read"); err != nil {
		// TODO: retry?
		fmt.Printf("failed to upload build output to S3: %s\n", err)
		return ""
	}
	return url
}

func (r *Runner) httpEventHandler(w http.ResponseWriter, req *http.Request) {
	header, ok := req.Header["X-Github-Event"]
	if !ok {
		log.Println("webhook: request missing X-Github-Event header")
		http.Error(w, "missing X-Github-Event header\n", 400)
		return
	}

	name := strings.Join(header, " ")
	var event Event
	switch name {
	case "push":
		event = &PushEvent{}
	case "pull_request":
		event = &PullRequestEvent{}
	default:
		log.Println("webhook: unknown X-Github-Event:", name)
		http.Error(w, fmt.Sprintf("Unknown X-Github-Event: %s\n", name), 400)
		return
	}

	dec := json.NewDecoder(req.Body)
	if err := dec.Decode(&event); err != nil && err != io.EOF {
		log.Println("webhook: error decoding JSON", err)
		http.Error(w, fmt.Sprintf("invalid JSON payload for %s event", name), 400)
		return
	}
	repo := event.Repo()
	if _, ok := repos[repo]; !ok {
		log.Println("webhook: unknown repo", repo)
		http.Error(w, fmt.Sprintf("unknown repo %s", repo), 400)
		return
	}
	logEvent(event)
	r.events <- event
	io.WriteString(w, "ok\n")
}

func logEvent(event Event) {
	switch event.(type) {
	case *PushEvent:
		e := event.(*PushEvent)
		log.Printf(
			"received push of %s[%s] by %s: %s => %s\n",
			e.Repo(),
			e.Ref,
			e.Pusher.Name,
			e.Before,
			e.After,
		)
	case *PullRequestEvent:
		e := event.(*PullRequestEvent)
		log.Printf(
			"pull request %s/%d %s by %s\n",
			e.Repo(),
			e.Number,
			e.Action,
			e.Sender.Login,
		)
	}
}

func needsBuild(event Event) bool {
	if e, ok := event.(*PullRequestEvent); ok && e.Action == "closed" {
		return false
	}
	return true
}

type Status struct {
	State       string `json:"state"`
	TargetUrl   string `json:"target_url,omitempty"`
	Description string `json:"description,omitempty"`
	Context     string `json:"context,omitempty"`
}

var descriptions = map[string]string{
	"pending": "The Flynn CI build is in progress",
	"success": "The Flynn CI build passed",
	"failure": "The Flynn CI build failed",
}

func (r *Runner) updateStatus(repo, commit, state, targetUrl string) {
	go func() {
		log.Printf("updateStatus: %s %s[%s]\n", state, repo, commit)

		url := fmt.Sprintf("https://api.github.com/repos/flynn/%s/statuses/%s", repo, commit)
		status := Status{
			State:       state,
			TargetUrl:   targetUrl,
			Description: descriptions[state],
			Context:     "flynn",
		}
		body := bytes.NewBufferString("")
		if err := json.NewEncoder(body).Encode(status); err != nil {
			log.Printf("updateStatus: could not encode status: %+v\n", status)
			return
		}

		req, err := http.NewRequest("POST", url, body)
		if err != nil {
			log.Printf("updateStatus: could not create request: %s\n", err)
			return
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", fmt.Sprintf("token %s", r.githubToken))

		res, err := http.DefaultClient.Do(req)
		defer res.Body.Close()
		if err != nil {
			log.Printf("updateStatus: could not send request: %s\n", err)
			return
		}
		if res.StatusCode != 201 {
			log.Printf("updateStatus: request failed: %d\n", res.StatusCode)
		}
	}()
}

func (r *Runner) allocateNet() (string, error) {
	r.mtx.Lock()
	defer r.mtx.Unlock()
	for i := 0; i < 256; i++ {
		net := fmt.Sprintf("10.53.%d.1/24", i)
		if _, ok := r.networks[net]; !ok {
			r.networks[net] = struct{}{}
			return net, nil
		}
	}
	return "", errors.New("no available networks")
}

func (r *Runner) releaseNet(net string) {
	r.mtx.Lock()
	defer r.mtx.Unlock()
	delete(r.networks, net)
}
