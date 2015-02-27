package main

import (
	"bytes"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"path"
	"strconv"
	"strings"
	"sync"
	"text/template"
	"time"

	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/boltdb/bolt"
	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/cupcake/goamz/aws"
	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/cupcake/goamz/s3"
	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/flynn/tail"
	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/julienschmidt/httprouter"
	"github.com/flynn/flynn/pkg/attempt"
	"github.com/flynn/flynn/pkg/iotool"
	"github.com/flynn/flynn/pkg/random"
	"github.com/flynn/flynn/pkg/shutdown"
	"github.com/flynn/flynn/pkg/tlsconfig"
	"github.com/flynn/flynn/test/arg"
	"github.com/flynn/flynn/test/cluster"
)

var logBucket = "flynn-ci-logs"
var dbBucket = []byte("builds")
var listenPort string

const textPlain = "text/plain; charset=utf-8"

type Build struct {
	Id                string        `json:"id"`
	CreatedAt         *time.Time    `json:"created_at"`
	Commit            string        `json:"commit"`
	Merge             bool          `json:"merge"`
	State             string        `json:"state"`
	Description       string        `json:"description"`
	LogUrl            string        `json:"log_url"`
	LogFile           string        `json:"log_file"`
	Duration          time.Duration `json:"duration"`
	DurationFormatted string        `json:"duration_formatted"`
}

func newBuild(commit, description string, merge bool) *Build {
	now := time.Now()
	return &Build{
		Id:          now.Format("20060102150405") + "-" + random.String(8),
		CreatedAt:   &now,
		Commit:      commit,
		Description: description,
		Merge:       merge,
	}
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
	clusters    map[string]*cluster.Cluster
	authKey     string
	subnet      uint64
}

var args *arg.Args

const maxConcurrentBuilds = 3

func init() {
	args = arg.Parse()
	log.SetFlags(log.Lshortfile)
}

func main() {
	defer shutdown.Exit()
	runner := &Runner{
		bc:       args.BootConfig,
		events:   make(chan Event),
		networks: make(map[string]struct{}),
		buildCh:  make(chan struct{}, maxConcurrentBuilds),
		clusters: make(map[string]*cluster.Cluster),
	}
	if err := runner.start(); err != nil {
		shutdown.Fatal(err)
	}
}

func (r *Runner) start() error {
	r.authKey = os.Getenv("AUTH_KEY")
	if r.authKey == "" {
		return errors.New("AUTH_KEY not set")
	}

	r.githubToken = os.Getenv("GITHUB_TOKEN")
	if r.githubToken == "" {
		return errors.New("GITHUB_TOKEN not set")
	}

	awsAuth, err := aws.EnvAuth()
	if err != nil {
		return err
	}
	r.s3Bucket = s3.New(awsAuth, aws.USEast).Bucket(logBucket)

	_, listenPort, err = net.SplitHostPort(args.ListenAddr)
	if err != nil {
		return err
	}

	bc := r.bc
	bc.Network = r.allocateNet()
	if r.rootFS, err = cluster.BuildFlynn(bc, args.RootFS, "origin/master", false, os.Stdout); err != nil {
		return fmt.Errorf("could not build flynn: %s", err)
	}
	r.releaseNet(bc.Network)
	shutdown.BeforeExit(func() { removeRootFS(r.rootFS) })

	db, err := bolt.Open(args.DBPath, 0600, &bolt.Options{Timeout: 5 * time.Second})
	if err != nil {
		return fmt.Errorf("could not open db: %s", err)
	}
	r.db = db
	shutdown.BeforeExit(func() { r.db.Close() })

	if err := r.db.Update(func(tx *bolt.Tx) error {
		_, err := tx.CreateBucketIfNotExists(dbBucket)
		return err
	}); err != nil {
		return fmt.Errorf("could not create builds bucket: %s", err)
	}

	for i := 0; i < maxConcurrentBuilds; i++ {
		r.buildCh <- struct{}{}
	}

	if err := r.buildPending(); err != nil {
		log.Printf("could not build pending builds: %s", err)
	}

	go r.watchEvents()

	router := httprouter.New()
	router.RedirectTrailingSlash = true
	router.Handler("GET", "/", http.RedirectHandler("/builds", 302))
	router.POST("/", r.handleEvent)
	router.GET("/builds/:build", r.getBuildLog)
	router.POST("/builds/:build/restart", r.restartBuild)
	router.GET("/builds", r.getBuilds)
	router.ServeFiles("/assets/*filepath", http.Dir(args.AssetsDir))
	router.GET("/cluster/:cluster", r.clusterAPI(r.getCluster))
	router.POST("/cluster/:cluster", r.clusterAPI(r.addHost))
	router.POST("/cluster/:cluster/release", r.clusterAPI(r.addReleaseHosts))
	router.DELETE("/cluster/:cluster/:host", r.clusterAPI(r.removeHost))
	router.GET("/cluster/:cluster/dump-logs", r.clusterAPI(r.dumpLogs))

	srv := &http.Server{
		Addr:      args.ListenAddr,
		Handler:   router,
		TLSConfig: tlsconfig.SecureCiphers(nil),
	}
	log.Println("Listening on", args.ListenAddr, "...")
	if err := srv.ListenAndServeTLS(args.TLSCert, args.TLSKey); err != nil {
		return fmt.Errorf("ListenAndServeTLS: %s", err)
	}

	return nil
}

func (r *Runner) watchEvents() {
	for event := range r.events {
		if !needsBuild(event) {
			continue
		}
		_, merge := event.(*PullRequestEvent)
		b := newBuild(event.Commit(), event.String(), merge)
		go r.build(b)
	}
}

var testRunScript = template.Must(template.New("test-run").Parse(`
#!/bin/bash
set -e -x -o pipefail

echo {{ .Cluster.RouterIP }} {{ .Cluster.ClusterDomain }} {{ .Cluster.ControllerDomain }} | sudo tee -a /etc/hosts

cat > ~/.flynnrc

git config --global user.email "ci@flynn.io"
git config --global user.name "CI"

cd ~/go/src/github.com/flynn/flynn/test

cmd="bin/flynn-test \
  --flynnrc $HOME/.flynnrc \
  --cluster-api https://{{ .Cluster.BridgeIP }}:{{ .ListenPort }}/cluster/{{ .Cluster.ID }} \
  --cli $(pwd)/../cli/flynn-cli \
  --router-ip {{ .Cluster.RouterIP }} \
  --debug \
  --dump-logs"

timeout --signal=QUIT --kill-after=10 20m $cmd
`[1:]))

func formatDuration(d time.Duration) string {
	return fmt.Sprintf("%dm%02ds", d/time.Minute, d%time.Minute/time.Second)
}

func (r *Runner) build(b *Build) (err error) {
	logFile, err := ioutil.TempFile("", "build-log")
	if err != nil {
		return err
	}
	b.LogFile = logFile.Name()

	r.updateStatus(b, "pending", fmt.Sprintf("https://ci.flynn.io/builds/%s", b.Id))

	<-r.buildCh
	defer func() {
		r.buildCh <- struct{}{}
	}()

	start := time.Now()
	fmt.Fprintf(logFile, "Starting build of %s at %s\n", b.Commit, start.Format(time.RFC822))
	defer func() {
		b.Duration = time.Since(start)
		b.DurationFormatted = formatDuration(b.Duration)
		fmt.Fprintf(logFile, "build finished in %s\n", b.DurationFormatted)
		if err != nil {
			fmt.Fprintf(logFile, "build error: %s\n", err)
		}
		url := r.uploadToS3(logFile, b)
		logFile.Close()
		os.RemoveAll(b.LogFile)
		b.LogFile = ""
		if err == nil {
			log.Printf("build %s passed!\n", b.Id)
			r.updateStatus(b, "success", url)
		} else {
			log.Printf("build %s failed: %s\n", b.Id, err)
			r.updateStatus(b, "failure", url)
		}
	}()

	log.Printf("building %s\n", b.Commit)

	out := &iotool.SafeWriter{W: io.MultiWriter(os.Stdout, logFile)}
	bc := r.bc
	bc.Network = r.allocateNet()
	defer r.releaseNet(bc.Network)

	c := cluster.New(bc, out)
	log.Println("created cluster with ID", c.ID)
	r.clusters[c.ID] = c
	defer func() {
		delete(r.clusters, c.ID)
		c.Shutdown()
	}()

	rootFS, err := c.BuildFlynn(r.rootFS, b.Commit, b.Merge, true)
	defer removeRootFS(rootFS)
	if err != nil {
		return fmt.Errorf("could not build flynn: %s", err)
	}

	if _, err := c.Boot(3, out, true); err != nil {
		return fmt.Errorf("could not boot cluster: %s", err)
	}

	config, err := c.CLIConfig()
	if err != nil {
		return fmt.Errorf("could not generate flynnrc: %s", err)
	}

	var script bytes.Buffer
	testRunScript.Execute(&script, map[string]interface{}{"Cluster": c, "ListenPort": listenPort})
	return c.RunWithEnv(script.String(), &cluster.Streams{
		Stdin:  bytes.NewBuffer(config.Marshal()),
		Stdout: out,
		Stderr: out,
	}, map[string]string{"TEST_RUNNER_AUTH_KEY": r.authKey})
}

var s3attempts = attempt.Strategy{
	Min:   5,
	Total: time.Minute,
	Delay: time.Second,
}

func (r *Runner) uploadToS3(file *os.File, b *Build) string {
	name := fmt.Sprintf("%s-build-%s-%s.txt", b.Id, b.Commit, time.Now().Format("2006-01-02-15-04-05"))
	url := fmt.Sprintf("https://s3.amazonaws.com/%s/%s", logBucket, name)

	if _, err := file.Seek(0, os.SEEK_SET); err != nil {
		log.Printf("failed to seek log file: %s\n", err)
		return ""
	}

	stat, err := file.Stat()
	if err != nil {
		log.Printf("failed to get log file size: %s\n", err)
		return ""
	}

	log.Printf("uploading build log to S3: %s\n", url)
	if err := s3attempts.Run(func() error {
		return r.s3Bucket.PutReader(name, file, stat.Size(), textPlain, "public-read")
	}); err != nil {
		log.Printf("failed to upload build output to S3: %s\n", err)
	}
	return url
}

func (r *Runner) handleEvent(w http.ResponseWriter, req *http.Request, ps httprouter.Params) {
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

func (r *Runner) getBuilds(w http.ResponseWriter, req *http.Request, ps httprouter.Params) {
	w.Header().Set("Vary", "Accept")
	if !strings.Contains(req.Header.Get("Accept"), "application/json") {
		http.ServeFile(w, req, path.Join(args.AssetsDir, "index.html"))
		return
	}

	count := 10
	if n := req.FormValue("count"); n != "" {
		var err error
		if count, err = strconv.Atoi(n); err != nil {
			http.Error(w, "invalid count parameter\n", 400)
		}
	}
	var builds []*Build

	r.db.View(func(tx *bolt.Tx) error {
		c := tx.Bucket(dbBucket).Cursor()

		var k, v []byte
		if before := req.FormValue("before"); before != "" {
			c.Seek([]byte(before))
			k, v = c.Prev()
		} else {
			k, v = c.Last()
		}

		for i := 0; k != nil && i < count; k, v = c.Prev() {
			b := &Build{}
			if err := json.Unmarshal(v, b); err != nil {
				log.Printf("could not decode build %s: %s", v, err)
				continue
			}
			builds = append(builds, b)
			i++
		}
		return nil
	})

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(builds)
}

func (r *Runner) getBuildLog(w http.ResponseWriter, req *http.Request, ps httprouter.Params) {
	id := ps.ByName("build")
	b := &Build{}
	if err := r.db.View(func(tx *bolt.Tx) error {
		v := tx.Bucket(dbBucket).Get([]byte(id))
		if err := json.Unmarshal(v, b); err != nil {
			return fmt.Errorf("could not decode build %s: %s", v, err)
		}
		return nil
	}); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	if b.LogFile == "" {
		http.Redirect(w, req, b.LogUrl, http.StatusMovedPermanently)
		return
	}
	t, err := tail.TailFile(b.LogFile, tail.Config{Follow: true, MustExist: true})
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	if cn, ok := w.(http.CloseNotifier); ok {
		go func() {
			<-cn.CloseNotify()
			t.Stop()
		}()
	} else {
		defer t.Stop()
	}
	flush := func() {
		if fw, ok := w.(http.Flusher); ok {
			fw.Flush()
		}
	}
	w.Header().Set("Content-Type", textPlain)
	w.WriteHeader(http.StatusOK)
	flush()
	for line := range t.Lines {
		if _, err := io.WriteString(w, line.Text+"\n"); err != nil {
			log.Printf("serveBuildLog write error: %s\n", err)
			return
		}
		flush()
		if strings.HasPrefix(line.Text, "build finished") {
			return
		}
	}
}

func (r *Runner) restartBuild(w http.ResponseWriter, req *http.Request, ps httprouter.Params) {
	id := ps.ByName("build")
	build := &Build{}
	if err := r.db.View(func(tx *bolt.Tx) error {
		val := tx.Bucket(dbBucket).Get([]byte(id))
		return json.Unmarshal(val, build)
	}); err != nil {
		http.Error(w, fmt.Sprintf("could not decode build %s: %s\n", id, err), 400)
		return
	}
	if build.State != "pending" {
		b := newBuild(build.Commit, "Restart: "+build.Description, build.Merge)
		go r.build(b)
	}
	http.Redirect(w, req, "/builds", 301)
}

func needsBuild(event Event) bool {
	if e, ok := event.(*PullRequestEvent); ok && e.Action == "closed" {
		return false
	}
	if e, ok := event.(*PushEvent); ok && (e.Deleted || e.Ref != "refs/heads/master") {
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
		log.Printf("updateStatus: %s %s\n", state, b.Commit)

		b.State = state
		b.LogUrl = targetUrl
		if err := r.save(b); err != nil {
			log.Printf("updateStatus: could not save build: %s", err)
		}

		url := fmt.Sprintf("https://api.github.com/repos/flynn/flynn/statuses/%s", b.Commit)
		status := Status{
			State:       state,
			TargetUrl:   targetUrl,
			Description: descriptions[state],
			Context:     "continuous-integration/flynn",
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
		if err != nil {
			log.Printf("updateStatus: could not send request: %s\n", err)
			return
		}
		res.Body.Close()
		if res.StatusCode != 201 {
			log.Printf("updateStatus: request failed: %d\n", res.StatusCode)
		}
	}()
}

func (r *Runner) allocateNet() string {
	r.netMtx.Lock()
	defer r.netMtx.Unlock()
	for {
		net := fmt.Sprintf("10.69.%d.1/24", r.subnet%256)
		r.subnet++
		if _, ok := r.networks[net]; !ok {
			r.networks[net] = struct{}{}
			return net
		}
	}
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
		go r.build(b)
	}
	return nil
}

func (r *Runner) save(b *Build) error {
	return r.db.Update(func(tx *bolt.Tx) error {
		val, err := json.Marshal(b)
		if err != nil {
			return err
		}
		return tx.Bucket(dbBucket).Put([]byte(b.Id), val)
	})
}

type clusterHandle func(*cluster.Cluster, http.ResponseWriter, url.Values, httprouter.Params) error

func (r *Runner) clusterAPI(handle clusterHandle) httprouter.Handle {
	return func(w http.ResponseWriter, req *http.Request, ps httprouter.Params) {
		_, authKey, ok := req.BasicAuth()
		if !ok || len(authKey) != len(r.authKey) || subtle.ConstantTimeCompare([]byte(authKey), []byte(r.authKey)) != 1 {
			w.WriteHeader(401)
			return
		}
		id := ps.ByName("cluster")
		c, ok := r.clusters[id]
		if !ok {
			http.Error(w, "cluster not found", 404)
			return
		}
		if err := handle(c, w, req.URL.Query(), ps); err != nil {
			http.Error(w, err.Error(), 500)
		}
	}
}

func (r *Runner) getCluster(c *cluster.Cluster, w http.ResponseWriter, q url.Values, ps httprouter.Params) error {
	return json.NewEncoder(w).Encode(c)
}

func (r *Runner) addHost(c *cluster.Cluster, w http.ResponseWriter, q url.Values, ps httprouter.Params) (err error) {
	var instance *cluster.Instance
	if q.Get("vanilla") == "" {
		instance, err = c.AddHost()
	} else {
		instance, err = c.AddVanillaHost(args.RootFS)
	}
	if err != nil {
		return err
	}
	return json.NewEncoder(w).Encode(instance)
}

func (r *Runner) addReleaseHosts(c *cluster.Cluster, w http.ResponseWriter, q url.Values, ps httprouter.Params) error {
	res, err := c.Boot(3, nil, true)
	if err != nil {
		return err
	}
	instance, err := c.AddVanillaHost(args.RootFS)
	if err != nil {
		return err
	}
	res.Instances = append(res.Instances, instance)
	return json.NewEncoder(w).Encode(res)
}

func (r *Runner) removeHost(c *cluster.Cluster, w http.ResponseWriter, q url.Values, ps httprouter.Params) error {
	hostID := ps.ByName("host")
	if err := c.RemoveHost(hostID); err != nil {
		return err
	}
	w.WriteHeader(200)
	return nil
}

func (r *Runner) dumpLogs(c *cluster.Cluster, w http.ResponseWriter, q url.Values, ps httprouter.Params) error {
	c.DumpLogs(w)
	return nil
}

func removeRootFS(path string) {
	fmt.Println("removing rootfs", path)
	if err := os.RemoveAll(path); err != nil {
		fmt.Println("could not remove rootfs", path, err)
		return
	}
	fmt.Println("rootfs removed", path)
}
