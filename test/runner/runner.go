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
	"strings"
	"sync"
	"text/template"
	"time"

	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/boltdb/bolt"
	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/cupcake/goamz/aws"
	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/cupcake/goamz/s3"
	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/gorilla/handlers"
	"github.com/flynn/flynn/pkg/attempt"
	"github.com/flynn/flynn/pkg/random"
	"github.com/flynn/flynn/test/arg"
	"github.com/flynn/flynn/test/cluster"
)

var logBucket = "flynn-ci-logs"
var dbBucket = []byte("builds")

type Build struct {
	Id          string     `json:"id"`
	CreatedAt   *time.Time `json:"created_at"`
	Repo        string     `json:"repo"`
	Commit      string     `json:"commit"`
	Merge       bool       `json:"merge"`
	State       string     `json:"state"`
	Description string     `json:"description"`
	LogUrl      string     `json:"log_url"`
}

type Runner struct {
	bc          cluster.BootConfig
	events      chan Event
	rootFS      string
	githubToken string
	s3Bucket    *s3.Bucket
	networks    map[string]struct{}
	netMtx      sync.Mutex
	db          *bolt.DB
	buildCh     chan struct{}
}

var args *arg.Args
var maxBuilds = 10

func init() {
	args = arg.Parse()
	log.SetFlags(log.Lshortfile)
}

func main() {
	runner := &Runner{
		bc:       args.BootConfig,
		events:   make(chan Event, 10),
		networks: make(map[string]struct{}),
		buildCh:  make(chan struct{}, maxBuilds),
	}
	if err := runner.start(); err != nil {
		log.Fatal(err)
	}
}

func (r *Runner) start() error {
	r.githubToken = os.Getenv("GITHUB_TOKEN")
	if r.githubToken == "" {
		return errors.New("GITHUB_TOKEN not set")
	}

	awsAuth, err := aws.EnvAuth()
	if err != nil {
		return err
	}
	r.s3Bucket = s3.New(awsAuth, aws.USEast).Bucket(logBucket)

	bc := r.bc
	bc.Network, err = r.allocateNet()
	if err != nil {
		return err
	}
	if r.rootFS, err = cluster.BuildFlynn(bc, args.RootFS, "origin/master", false, os.Stdout); err != nil {
		return fmt.Errorf("could not build flynn: %s", err)
	}
	r.releaseNet(bc.Network)
	defer os.RemoveAll(r.rootFS)

	db, err := bolt.Open(args.DBPath, 0600, &bolt.Options{Timeout: 5 * time.Second})
	if err != nil {
		return fmt.Errorf("could not open db: %s", err)
	}
	r.db = db
	defer r.db.Close()

	if err := r.db.Update(func(tx *bolt.Tx) error {
		_, err := tx.CreateBucketIfNotExists(dbBucket)
		return err
	}); err != nil {
		return fmt.Errorf("could not create builds bucket: %s", err)
	}

	for i := 0; i < maxBuilds; i++ {
		r.buildCh <- struct{}{}
	}

	if err := r.buildPending(); err != nil {
		log.Printf("could not build pending builds: %s", err)
	}

	go r.watchEvents()

	http.Handle("/", handlers.CombinedLoggingHandler(os.Stdout, http.HandlerFunc(r.httpEventHandler)))
	http.Handle("/builds", handlers.CombinedLoggingHandler(os.Stdout, http.HandlerFunc(r.httpBuildHandler)))
	log.Println("Listening on", args.ListenAddr, "...")
	if err := http.ListenAndServeTLS(args.ListenAddr, args.TLSCert, args.TLSKey, nil); err != nil {
		return fmt.Errorf("ListenAndServeTLS: %s", err)
	}
	return nil
}

func (r *Runner) watchEvents() {
	for event := range r.events {
		if !needsBuild(event) {
			continue
		}
		go func(event Event) {
			now := time.Now()
			b := &Build{
				CreatedAt:   &now,
				Repo:        event.Repo(),
				Commit:      event.Commit(),
				Description: event.String(),
			}
			if _, ok := event.(*PullRequestEvent); ok {
				b.Merge = true
			}
			if err := r.build(b); err != nil {
				log.Printf("build %s failed: %s\n", b.Id, err)
				return
			}
			log.Printf("build %s passed!\n", b.Id)
		}(event)
	}
}

var testRunScript = template.Must(template.New("test-run").Parse(`
#!/bin/bash
set -e -x -o pipefail

echo {{ .RouterIP }} {{ .ControllerDomain }} | sudo tee -a /etc/hosts

cat > ~/.flynnrc

git config --global user.email "ci@flynn.io"
git config --global user.name "CI"

cd ~/go/src/github.com/flynn/flynn/test

bin/flynn-test \
  --flynnrc ~/.flynnrc \
  --cli $(pwd)/../cli/flynn-cli \
  --router-ip {{ .RouterIP }} \
  --debug
`[1:]))

func (r *Runner) build(b *Build) (err error) {
	r.updateStatus(b, "pending", "")

	<-r.buildCh
	defer func() {
		r.buildCh <- struct{}{}
	}()

	var buildLog bytes.Buffer
	defer func() {
		if err != nil {
			fmt.Fprintf(&buildLog, "build error: %s\n", err)
		}
		url := r.uploadToS3(buildLog, b)
		if err == nil {
			r.updateStatus(b, "success", url)
		} else {
			r.updateStatus(b, "failure", url)
		}
	}()

	log.Printf("building %s[%s]\n", b.Repo, b.Commit)

	out := io.MultiWriter(os.Stdout, &buildLog)
	bc := r.bc
	bc.Network, err = r.allocateNet()
	if err != nil {
		return err
	}
	defer r.releaseNet(bc.Network)

	c := cluster.New(bc, out)
	defer c.Shutdown()

	rootFS, err := c.BuildFlynn(r.rootFS, b.Commit, b.Merge)
	defer os.RemoveAll(rootFS)
	if err != nil {
		return fmt.Errorf("could not build flynn: %s", err)
	}

	if err := c.Boot(args.Backend, rootFS, 1); err != nil {
		return fmt.Errorf("could not boot cluster: %s", err)
	}

	config, err := c.CLIConfig()
	if err != nil {
		return fmt.Errorf("could not generate flynnrc: %s", err)
	}

	var script bytes.Buffer
	testRunScript.Execute(&script, c)
	return c.Run(script.String(), &cluster.Streams{
		Stdin:  bytes.NewBuffer(config.Marshal()),
		Stdout: out,
		Stderr: out,
	})
}

var s3attempts = attempt.Strategy{
	Min:   5,
	Total: time.Minute,
	Delay: time.Second,
}

func (r *Runner) uploadToS3(buildLog bytes.Buffer, b *Build) string {
	name := fmt.Sprintf("%s-build-%s-%s-%s.txt", b.Repo, b.Id, b.Commit, time.Now().Format("2006-01-02-15-04-05"))
	url := fmt.Sprintf("https://s3.amazonaws.com/%s/%s", logBucket, name)
	log.Printf("uploading build log to S3: %s\n", url)
	if err := s3attempts.Run(func() error {
		return r.s3Bucket.Put(name, buildLog.Bytes(), "text/plain", "public-read")
	}); err != nil {
		log.Printf("failed to upload build output to S3: %s\n", err)
	}
	return url
}

func (r *Runner) httpEventHandler(w http.ResponseWriter, req *http.Request) {
	if req.Method == "GET" {
		http.Redirect(w, req, "/builds", 302)
		return
	}

	header, ok := req.Header["X-Github-Event"]
	if !ok {
		log.Println("webhook: request missing X-Github-Event header")
		http.Error(w, "missing X-Github-Event header\n", 400)
		return
	}

	name := strings.Join(header, " ")
	var event Event
	switch name {
	case "ping":
		io.WriteString(w, "pong\n")
		return
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
	if repo != "flynn" {
		log.Println("webhook: unknown repo", repo)
		http.Error(w, fmt.Sprintf("unknown repo %s", repo), 400)
		return
	}
	log.Println(event)
	r.events <- event
	io.WriteString(w, "ok\n")
}

var formatters = template.FuncMap{
	"formatTime": func(t time.Time) string { return t.Format(time.RFC822) },
}

var buildsPage = template.Must(template.New("builds-page").Funcs(formatters).Parse(`
<html>
  <head>
    <link rel="stylesheet" href="//maxcdn.bootstrapcdn.com/bootstrap/3.2.0/css/bootstrap.min.css">
  </head>

  <body>
    <div class="container">
      <h1>Flynn CI Builds</h1>

      <table class="table">
        <thead>
          <tr>
            <th>Commit</th>
            <th>Description</th>
            <th>Created</th>
            <th>State</th>
            <th></th>
          </tr>
        </thead>

        <tbody>
          {{ range . }}
            <tr>
              <td>
                {{ if .LogUrl }}
                  <a href="{{ .LogUrl }}">{{ .Commit }}</a>
                {{ else }}
                  {{ .Commit }}
                {{ end }}
              </td>
              <td>{{ .Description }}</td>
              <td>{{ .CreatedAt | formatTime }}</td>
              <td>{{ .State }}</td>
              <td>
                <form action="/builds" method="POST">
                  <input type="hidden" name="id" value="{{ .Id }}" />
                  <input type="submit" value="Rebuild" class="btn" />
                </form>
              </td>
            </tr>
          {{ end }}
        </tbody>
      </table>
    </div>
  </body>
</html>
`))

func (r *Runner) httpBuildHandler(w http.ResponseWriter, req *http.Request) {
	if req.Method == "POST" {
		r.handleBuildRequest(w, req)
		return
	}

	var builds []*Build

	r.db.View(func(tx *bolt.Tx) error {
		c := tx.Bucket(dbBucket).Cursor()
		for k, v := c.Last(); k != nil; k, v = c.Prev() {
			b := &Build{}
			if err := json.Unmarshal(v, b); err != nil {
				log.Printf("could not decode build %s: %s", v, err)
				continue
			}
			builds = append(builds, b)
		}
		return nil
	})

	w.Header().Set("Content-Type", "text/html")
	buildsPage.Execute(w, builds)
}

func (r *Runner) handleBuildRequest(w http.ResponseWriter, req *http.Request) {
	id := req.FormValue("id")
	if id == "" {
		http.Error(w, "missing id parameter\n", 400)
		return
	}
	b := &Build{}
	if err := r.db.View(func(tx *bolt.Tx) error {
		val := tx.Bucket(dbBucket).Get([]byte(id))
		return json.Unmarshal(val, b)
	}); err != nil {
		http.Error(w, fmt.Sprintf("could not decode build %s: %s\n", id, err), 400)
		return
	}
	if b.State != "pending" {
		go func() {
			if err := r.build(b); err != nil {
				log.Printf("build %s failed: %s\n", b.Id, err)
				return
			}
			log.Printf("build %s passed!\n", b.Id)
		}()
	}
	http.Redirect(w, req, "/builds", 301)
}

func needsBuild(event Event) bool {
	if e, ok := event.(*PullRequestEvent); ok && e.Action == "closed" {
		return false
	}
	if e, ok := event.(*PushEvent); ok && e.Deleted {
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

func (r *Runner) updateStatus(b *Build, state, targetUrl string) {
	go func() {
		log.Printf("updateStatus: %s %s[%s]\n", state, b.Repo, b.Commit)

		b.State = state
		b.LogUrl = targetUrl
		if err := r.save(b); err != nil {
			log.Printf("updateStatus: could not save build: %s", err)
		}

		url := fmt.Sprintf("https://api.github.com/repos/flynn/%s/statuses/%s", b.Repo, b.Commit)
		status := Status{
			State:       state,
			TargetUrl:   targetUrl,
			Description: descriptions[state],
			Context:     "flynn",
		}
		body := &bytes.Buffer{}
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
		req.Header.Set("Authorization", "token "+r.githubToken)

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
	r.netMtx.Lock()
	defer r.netMtx.Unlock()
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
	r.netMtx.Lock()
	defer r.netMtx.Unlock()
	delete(r.networks, net)
}

func (r *Runner) buildPending() error {
	var pending []*Build

	r.db.View(func(tx *bolt.Tx) error {
		return tx.Bucket(dbBucket).ForEach(func(k, v []byte) error {
			b := &Build{}
			if err := json.Unmarshal(v, b); err != nil {
				log.Printf("could not decode build %s: %s", v, err)
				return nil
			}
			if b.State == "pending" {
				pending = append(pending, b)
			}
			return nil
		})
	})

	for _, b := range pending {
		go func(b *Build) {
			if err := r.build(b); err != nil {
				log.Printf("build %s failed: %s\n", b.Id, err)
				return
			}
			log.Printf("build %s passed!\n", b.Id)
		}(b)
	}
	return nil
}

func (r *Runner) save(b *Build) error {
	if b.Id == "" {
		b.Id = b.CreatedAt.Format("20060102150405") + "-" + random.String(8)
	}
	return r.db.Update(func(tx *bolt.Tx) error {
		val, err := json.Marshal(b)
		if err != nil {
			return err
		}
		return tx.Bucket(dbBucket).Put([]byte(b.Id), val)
	})
}
