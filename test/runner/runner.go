package main

import (
	"bufio"
	"bytes"
	"crypto/subtle"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"mime"
	"mime/multipart"
	"net"
	"net/http"
	"net/url"
	"os"
	"path"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"text/template"
	"time"

	"github.com/awslabs/aws-sdk-go/aws"
	"github.com/awslabs/aws-sdk-go/gen/s3"
	"github.com/boltdb/bolt"
	"github.com/flynn/flynn/pkg/attempt"
	"github.com/flynn/flynn/pkg/iotool"
	"github.com/flynn/flynn/pkg/random"
	"github.com/flynn/flynn/pkg/shutdown"
	"github.com/flynn/flynn/pkg/sse"
	"github.com/flynn/flynn/pkg/stream"
	"github.com/flynn/flynn/pkg/tlsconfig"
	"github.com/flynn/flynn/pkg/typeconv"
	"github.com/flynn/flynn/test/arg"
	"github.com/flynn/flynn/test/buildlog"
	"github.com/flynn/flynn/test/cluster"
	"github.com/flynn/tail"
	"github.com/julienschmidt/httprouter"
	"github.com/thoj/go-ircevent"
	"golang.org/x/crypto/acme/autocert"
)

var logBucket = "flynn-ci-logs"
var dbBucket = []byte("builds")
var listenPort string

const textPlain = "text/plain; charset=utf-8"

type BuildVersion string

const (
	// BuildVersion1 represents builds which store raw logs in S3, so users
	// are redirected to S3 when viewing logs.
	BuildVersion1 BuildVersion = "v1"

	// BuildVersion2 represents builds which store logs in multipart format
	// in S3, so are compatible with the scrolling logs page.
	BuildVersion2 BuildVersion = "v2"
)

type Build struct {
	ID                string        `json:"id"`
	CreatedAt         *time.Time    `json:"created_at"`
	Commit            string        `json:"commit"`
	Branch            string        `json:"branch"`
	Merge             bool          `json:"merge"`
	State             string        `json:"state"`
	Description       string        `json:"description"`
	LogURL            string        `json:"log_url"`
	LogFile           string        `json:"log_file"`
	Duration          time.Duration `json:"duration"`
	DurationFormatted string        `json:"duration_formatted"`
	Reason            string        `json:"reason"`
	IssueLink         string        `json:"issue_link"`
	Version           BuildVersion  `json:"version"`
	Failures          []string      `json:"failures"`
}

func (b *Build) URL() string {
	if listenPort != "" && listenPort != "443" {
		return "https://ci.flynn.io:" + listenPort + "/builds/" + b.ID
	}
	return "https://ci.flynn.io/builds/" + b.ID
}

func (b *Build) Finished() bool {
	return b.State != "pending"
}

func (b *Build) StateLabelClass() string {
	switch b.State {
	case "success":
		return "label-success"
	case "pending":
		return "label-info"
	case "failure":
		return "label-danger"
	default:
		return ""
	}
}

func newBuild(commit, branch, description string, merge bool) *Build {
	now := time.Now()
	return &Build{
		ID:          now.Format("20060102150405") + "-" + random.String(8),
		CreatedAt:   &now,
		Commit:      commit,
		Branch:      branch,
		Description: description,
		Merge:       merge,
		Version:     BuildVersion2,
	}
}

type Runner struct {
	bc          cluster.BootConfig
	events      chan Event
	rootFS      string
	githubToken string
	s3          *s3.S3
	networks    map[string]struct{}
	netMtx      sync.Mutex
	db          *bolt.DB
	buildCh     chan struct{}
	clusters    map[string]*cluster.Cluster
	authKey     string
	runEnv      map[string]string
	subnet      uint64
	ircMsgs     chan string
}

var args *arg.Args

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
		buildCh:  make(chan struct{}, args.ConcurrentBuilds),
		clusters: make(map[string]*cluster.Cluster),
		ircMsgs:  make(chan string, 100),
		runEnv:   make(map[string]string),
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
	r.runEnv["TEST_RUNNER_AUTH_KEY"] = r.authKey

	for _, s := range []string{"S3", "GCS", "AZURE"} {
		name := fmt.Sprintf("BLOBSTORE_%s_CONFIG", s)
		if c := os.Getenv(name); c != "" {
			r.runEnv[name] = c
		} else {
			return fmt.Errorf("%s not set", name)
		}
	}

	r.githubToken = os.Getenv("GITHUB_TOKEN")
	if r.githubToken == "" {
		return errors.New("GITHUB_TOKEN not set")
	}

	am := autocert.Manager{
		Prompt:     autocert.AcceptTOS,
		Cache:      autocert.DirCache(args.TLSDir),
		HostPolicy: autocert.HostWhitelist(args.Domain),
	}

	awsAuth, err := aws.EnvCreds()
	if err != nil {
		return err
	}
	r.s3 = s3.New(awsAuth, "us-east-1", nil)

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

	for i := 0; i < args.ConcurrentBuilds; i++ {
		r.buildCh <- struct{}{}
	}

	if err := r.buildPending(); err != nil {
		log.Printf("could not build pending builds: %s", err)
	}

	go r.connectIRC()
	go r.watchEvents()

	router := httprouter.New()
	router.RedirectTrailingSlash = true
	router.Handler("GET", "/", http.RedirectHandler("/builds", 302))
	router.POST("/", r.handleEvent)
	router.GET("/builds/:build", r.getBuildLog)
	router.GET("/builds/:build/download", r.downloadBuildLog)
	router.POST("/builds/:build/restart", r.restartBuild)
	router.POST("/builds/:build/explain", r.explainBuild)
	router.GET("/builds", r.getBuilds)
	router.ServeFiles("/assets/*filepath", http.Dir(args.AssetsDir))
	router.GET("/cluster/:cluster", r.clusterAPI(r.getCluster))
	router.POST("/cluster/:cluster", r.clusterAPI(r.addHost))
	router.POST("/cluster/:cluster/release", r.clusterAPI(r.addReleaseHosts))
	router.DELETE("/cluster/:cluster/:host", r.clusterAPI(r.removeHost))

	srv := &http.Server{
		Addr:    args.ListenAddr,
		Handler: router,
		TLSConfig: tlsconfig.SecureCiphers(&tls.Config{
			GetCertificate: am.GetCertificate,
		}),
	}
	log.Println("Listening on", args.ListenAddr, "...")
	if err := srv.ListenAndServeTLS("", ""); err != nil {
		return fmt.Errorf("ListenAndServeTLS: %s", err)
	}

	return nil
}

const (
	ircServer = "irc.freenode.net:6697"
	ircNick   = "flynn-ci"
	ircRoom   = "#flynn"
)

func (r *Runner) connectIRC() {
	conn := irc.IRC(ircNick, ircNick)
	conn.UseTLS = true
	conn.AddCallback("001", func(*irc.Event) {
		conn.Join(ircRoom)
	})
	var once sync.Once
	ready := make(chan struct{})
	conn.AddCallback("JOIN", func(*irc.Event) {
		once.Do(func() { close(ready) })
	})
	for {
		log.Printf("connecting to IRC server: %s", ircServer)
		err := conn.Connect(ircServer)
		if err == nil {
			break
		}
		log.Printf("error connecting to IRC server: %s", err)
		time.Sleep(time.Second)
	}
	go conn.Loop()
	<-ready
	for msg := range r.ircMsgs {
		conn.Notice(ircRoom, msg)
	}
}

func (r *Runner) postIRC(format string, v ...interface{}) {
	go func() {
		// drop the message if the buffer is full because we are
		// reconnecting
		select {
		case r.ircMsgs <- fmt.Sprintf(format, v...):
		default:
		}
	}()
}

func (r *Runner) watchEvents() {
	for event := range r.events {
		if !needsBuild(event) {
			continue
		}
		_, merge := event.(*PullRequestEvent)
		b := newBuild(event.Commit(), event.Branch(), event.String(), merge)
		go r.build(b)
	}
}

var testRunScript = template.Must(template.New("test-run").Parse(`
#!/bin/bash
set -e -x -o pipefail

echo {{ .Cluster.RouterIP }} {{ .Cluster.ClusterDomain }} {{ .Cluster.ControllerDomain }} {{ .Cluster.GitDomain }} dashboard.{{ .Cluster.ClusterDomain }} docker.{{ .Cluster.ClusterDomain }} | sudo tee -a /etc/hosts

# Wait for the Flynn bridge interface to show up so we can use it as the
# nameserver to resolve discoverd domains
iface=flynnbr0
start=$(date +%s)
while true; do
  ip=$(ifconfig ${iface} | grep -oP 'inet addr:\S+' | cut -d: -f2)
  [[ -n "${ip}" ]] && break || sleep 0.2

  elapsed=$(($(date +%s) - ${start}))
  if [[ ${elapsed} -gt 60 ]]; then
    echo "${iface} did not appear within 60 seconds"
    exit 1
  fi
done

echo "nameserver ${ip}" | sudo tee /etc/resolv.conf

cd ~/go/src/github.com/flynn/flynn

script/configure-docker "{{ .Cluster.ClusterDomain }}"

cli/bin/flynn cluster add \
  --tls-pin "{{ .Config.TLSPin }}" \
  --git-url "{{ .Config.GitURL }}" \
  --docker-push-url "{{ .Config.DockerPushURL }}" \
  default \
  {{ .Config.ControllerURL }} \
  {{ .Config.Key }}

git config --global user.email "ci@flynn.io"
git config --global user.name "CI"

cd test

cmd="bin/flynn-test \
  --flynnrc $HOME/.flynnrc \
  --cluster-api https://{{ .Cluster.BridgeIP }}:{{ .ListenPort }}/cluster/{{ .Cluster.ID }} \
  --cli $(pwd)/../cli/bin/flynn \
  --flynn-host $(pwd)/../host/bin/flynn-host \
  --router-ip {{ .Cluster.RouterIP }} \
  --backups-dir "/mnt/backups" \
  --debug"

timeout --signal=QUIT --kill-after=10 45m $cmd
`[1:]))

func formatDuration(d time.Duration) string {
	return fmt.Sprintf("%dm%02ds", d/time.Minute, d%time.Minute/time.Second)
}

// failPattern matches failed test output like:
// 19:53:03.590 FAIL: test_scheduler.go:221: SchedulerSuite.TestOmniJobs
var failPattern = regexp.MustCompile(`^\d{2}:\d{2}:\d{2}\.\d{3} FAIL: (?:\S+) (\S+)$`)

func (r *Runner) build(b *Build) (err error) {
	logFile, err := ioutil.TempFile("", "build-log")
	if err != nil {
		return err
	}
	b.LogFile = logFile.Name()

	buildLog := buildlog.NewLog(logFile)
	mainLog, err := buildLog.NewFile("build.log")
	if err != nil {
		return err
	}

	r.updateStatus(b, "pending")

	<-r.buildCh
	defer func() {
		r.buildCh <- struct{}{}
	}()

	start := time.Now()
	fmt.Fprintf(mainLog, "Starting build of %s at %s\n", b.Commit, start.Format(time.RFC822))
	var c *cluster.Cluster
	var failureBuf bytes.Buffer
	defer func() {
		// parse the failures
		s := bufio.NewScanner(&failureBuf)
		for s.Scan() {
			if match := failPattern.FindSubmatch(s.Bytes()); match != nil {
				b.Failures = append(b.Failures, string(match[1]))
			}
		}

		b.Duration = time.Since(start)
		b.DurationFormatted = formatDuration(b.Duration)
		fmt.Fprintf(mainLog, "build finished in %s\n", b.DurationFormatted)
		if err != nil {
			fmt.Fprintf(mainLog, "build error: %s\n", err)
			fmt.Fprintln(mainLog, "DUMPING LOGS")
			c.DumpLogs(buildLog)
		}
		c.Shutdown()
		buildLog.Close()
		b.LogURL = r.uploadToS3(logFile, b, buildLog.Boundary())
		logFile.Close()
		os.RemoveAll(b.LogFile)
		b.LogFile = ""
		if err == nil {
			log.Printf("build %s passed!\n", b.ID)
			r.updateStatus(b, "success")
			r.postIRC("PASS: %s %s", b.Description, b.URL())
		} else {
			log.Printf("build %s failed: %s\n", b.ID, err)
			r.updateStatus(b, "failure")
			r.postIRC("FAIL: [%d failure(s)] %s %s", len(b.Failures), b.Description, b.URL())
		}
	}()

	log.Printf("building %s\n", b.Commit)

	out := &iotool.SafeWriter{W: io.MultiWriter(os.Stdout, mainLog, &failureBuf)}
	bc := r.bc
	bc.Network = r.allocateNet()
	defer r.releaseNet(bc.Network)

	c = cluster.New(bc, out)
	log.Println("created cluster with ID", c.ID)
	r.clusters[c.ID] = c
	defer func() {
		delete(r.clusters, c.ID)
	}()

	rootFS, err := c.BuildFlynn(r.rootFS, b.Commit, b.Merge, true)
	defer removeRootFS(rootFS)
	if err != nil {
		return fmt.Errorf("could not build flynn: %s", err)
	}

	if _, err := c.Boot(cluster.ClusterTypeDefault, 3, buildLog, false); err != nil {
		return fmt.Errorf("could not boot cluster: %s", err)
	}

	config, err := c.CLIConfig()
	if err != nil {
		return fmt.Errorf("could not generate flynnrc: %s", err)
	}

	var script bytes.Buffer
	testRunScript.Execute(&script, map[string]interface{}{"Cluster": c, "Config": config.Clusters[0], "ListenPort": listenPort})
	return c.RunWithEnv(script.String(), &cluster.Streams{Stdout: out, Stderr: out}, r.runEnv)
}

var s3attempts = attempt.Strategy{
	Min:   5,
	Total: time.Minute,
	Delay: time.Second,
}

func (r *Runner) uploadToS3(file *os.File, b *Build, boundary string) string {
	name := fmt.Sprintf("%s-build-%s-%s.txt", b.ID, b.Commit, time.Now().Format("2006-01-02-15-04-05"))
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
		contentType := "multipart/mixed; boundary=" + boundary
		acl := "public-read"
		_, err := r.s3.PutObject(&s3.PutObjectRequest{
			Key:           &name,
			Body:          file,
			Bucket:        &logBucket,
			ACL:           &acl,
			ContentType:   &contentType,
			ContentLength: typeconv.Int64Ptr(stat.Size()),
		})
		return err
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

func getBuildLogStream(b *Build, ch chan string) (stream.Stream, error) {
	stream := stream.New()

	// if the build hasn't finished, tail the log from disk
	if !b.Finished() {
		t, err := tail.TailFile(b.LogFile, tail.Config{Follow: true, MustExist: true})
		if err != nil {
			return nil, err
		}
		go func() {
			defer t.Stop()
			defer close(ch)
			for {
				select {
				case line, ok := <-t.Lines:
					if !ok {
						stream.Error = t.Err()
						return
					}
					select {
					case ch <- line.Text:
					case <-stream.StopCh:
						return
					}
					if strings.HasPrefix(line.Text, "build finished") {
						return
					}
				case <-stream.StopCh:
					return
				}
			}
		}()
		return stream, nil
	}

	// get the multipart log from S3 and serve just the "build.log" file
	res, err := http.Get(b.LogURL)
	if err != nil {
		return nil, err
	}
	if res.StatusCode != http.StatusOK {
		res.Body.Close()
		return nil, fmt.Errorf("unexpected status %d getting build log", res.StatusCode)
	}
	_, params, err := mime.ParseMediaType(res.Header.Get("Content-Type"))
	if err != nil {
		res.Body.Close()
		return nil, err
	}
	go func() {
		defer res.Body.Close()
		defer close(ch)

		mr := multipart.NewReader(res.Body, params["boundary"])
		for {
			select {
			case <-stream.StopCh:
				return
			default:
			}

			p, err := mr.NextPart()
			if err != nil {
				stream.Error = err
				return
			}
			if p.FileName() != "build.log" {
				continue
			}
			s := bufio.NewScanner(p)
			for s.Scan() {
				select {
				case ch <- s.Text():
				case <-stream.StopCh:
					return
				}
			}
			return
		}
	}()
	return stream, nil
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

	// if it's a V1 build, redirect to the log in S3
	if b.Version == BuildVersion1 {
		http.Redirect(w, req, b.LogURL, http.StatusMovedPermanently)
		return
	}

	// if it's a browser, serve the build-log.html template
	if strings.Contains(req.Header.Get("Accept"), "text/html") {
		tpl, err := template.ParseFiles(path.Join(args.AssetsDir, "build-log.html"))
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if err := tpl.Execute(w, b); err != nil {
			log.Printf("error executing build-log template: %s", err)
		}
		return
	}

	// serve the build log as either an SSE or plain text stream
	ch := make(chan string)
	stream, err := getBuildLogStream(b, ch)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	if cn, ok := w.(http.CloseNotifier); ok {
		go func() {
			<-cn.CloseNotify()
			stream.Close()
		}()
	} else {
		defer stream.Close()
	}

	if strings.Contains(req.Header.Get("Accept"), "text/event-stream") {
		sse.ServeStream(w, ch, nil)
	} else {
		servePlainStream(w, ch)
	}

	if err := stream.Err(); err != nil {
		log.Println("error serving build log stream:", err)
	}
}

func servePlainStream(w http.ResponseWriter, ch chan string) {
	flush := func() {
		if fw, ok := w.(http.Flusher); ok {
			fw.Flush()
		}
	}
	w.Header().Set("Content-Type", textPlain)
	w.WriteHeader(http.StatusOK)
	flush()
	for line := range ch {
		if _, err := io.WriteString(w, line+"\n"); err != nil {
			log.Println("servePlainStream write error:", err)
			return
		}
		flush()
	}
}

func (r *Runner) downloadBuildLog(w http.ResponseWriter, req *http.Request, ps httprouter.Params) {
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

	res, err := http.Get(b.LogURL)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		http.Error(w, fmt.Sprintf("unexpected status %d getting build log", res.StatusCode), 500)
		return
	}

	_, params, err := mime.ParseMediaType(res.Header.Get("Content-Type"))
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}

	// although the log is multipart, serve it as a single plain text file
	// to avoid browsers potentially prompting to download each individual
	// part, but construct valid multipart content, headers included, so
	// the file can be parsed with tools such as munpack(1).
	w.Header().Set("Content-Type", textPlain)
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=\"flynn-ci-build-%s\"", path.Base(b.LogURL)))
	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, "MIME-Version: 1.0\r\nContent-Type: multipart/mixed; boundary=%q\r\n\r\n", params["boundary"])
	io.Copy(w, res.Body)
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
		desc := build.Description
		if !strings.HasPrefix(desc, "Restart: ") {
			desc = "Restart: " + desc
		}
		b := newBuild(build.Commit, build.Branch, desc, build.Merge)
		go r.build(b)
	}
	http.Redirect(w, req, "/builds", 301)
}

func (r *Runner) explainBuild(w http.ResponseWriter, req *http.Request, ps httprouter.Params) {
	id := ps.ByName("build")
	build := &Build{}
	if err := r.db.View(func(tx *bolt.Tx) error {
		val := tx.Bucket(dbBucket).Get([]byte(id))
		return json.Unmarshal(val, build)
	}); err != nil {
		http.Error(w, fmt.Sprintf("could not decode build %s: %s\n", id, err), 400)
		return
	}
	build.Reason = req.FormValue("reason")
	build.IssueLink = req.FormValue("issue-link")
	if build.IssueLink != "" && !strings.HasPrefix(build.IssueLink, "https://github.com/flynn/flynn/issues/") {
		http.Error(w, fmt.Sprintf("Invalid GitHub issue link: %q\n", build.IssueLink), 400)
		return
	}
	if err := r.save(build); err != nil {
		http.Error(w, fmt.Sprintf("error saving build: %s\n", err), 500)
		return
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
	TargetURL   string `json:"target_url,omitempty"`
	Description string `json:"description,omitempty"`
	Context     string `json:"context,omitempty"`
}

var descriptions = map[string]string{
	"pending": "The Flynn CI build is in progress",
	"success": "The Flynn CI build passed",
	"failure": "The Flynn CI build failed",
}

func (r *Runner) updateStatus(b *Build, state string) {
	go func() {
		log.Printf("updateStatus: %s %s\n", state, b.Commit)

		b.State = state
		if err := r.save(b); err != nil {
			log.Printf("updateStatus: could not save build: %s", err)
		}

		url := fmt.Sprintf("https://api.github.com/repos/flynn/flynn/statuses/%s", b.Commit)
		description := descriptions[state]
		if len(b.Failures) > 0 {
			description += fmt.Sprintf(" [%d failure(s)]", len(b.Failures))
		}
		status := Status{
			State:       state,
			TargetURL:   b.URL(),
			Description: description,
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
		return tx.Bucket(dbBucket).Put([]byte(b.ID), val)
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
			log.Printf("clusterAPI err in %s %s: %s", req.Method, req.URL.Path, err)
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
	res, err := c.Boot(cluster.ClusterTypeRelease, 3, nil, false)
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

func removeRootFS(path string) {
	fmt.Println("removing rootfs", path)
	if err := os.RemoveAll(path); err != nil {
		fmt.Println("could not remove rootfs", path, err)
		return
	}
	fmt.Println("rootfs removed", path)
}
