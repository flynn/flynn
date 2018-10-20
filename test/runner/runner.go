package main

import (
	"bufio"
	"bytes"
	"crypto/subtle"
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
	"text/template"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/flynn/flynn/pkg/attempt"
	"github.com/flynn/flynn/pkg/httphelper"
	"github.com/flynn/flynn/pkg/iotool"
	"github.com/flynn/flynn/pkg/postgres"
	"github.com/flynn/flynn/pkg/random"
	"github.com/flynn/flynn/pkg/shutdown"
	"github.com/flynn/flynn/pkg/sse"
	"github.com/flynn/flynn/pkg/stream"
	"github.com/flynn/flynn/pkg/typeconv"
	"github.com/flynn/flynn/test/arg"
	"github.com/flynn/flynn/test/buildlog"
	"github.com/flynn/flynn/test/cluster"
	"github.com/flynn/tail"
	"github.com/julienschmidt/httprouter"
)

var logBucket = "flynn-ci-logs"

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
	ID          string       `json:"id"`
	CreatedAt   *time.Time   `json:"created_at"`
	Commit      string       `json:"commit"`
	Branch      string       `json:"branch"`
	Merge       bool         `json:"merge"`
	State       string       `json:"state"`
	Description string       `json:"description"`
	LogURL      string       `json:"log_url"`
	LogFile     string       `json:"log_file"`
	Duration    string       `json:"duration"`
	Reason      string       `json:"reason"`
	IssueLink   string       `json:"issue_link"`
	Version     BuildVersion `json:"version"`
	Failures    []string     `json:"failures"`
}

func (b *Build) URL() string {
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
	db          *postgres.DB
	buildCh     chan struct{}
	clusters    map[string]*cluster.Cluster
	authKey     string
	runEnv      map[string]string
	ip          string
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
		buildCh:  make(chan struct{}, args.ConcurrentBuilds),
		clusters: make(map[string]*cluster.Cluster),
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

	// set r.ip from /.containerconfig
	if err := func() error {
		f, err := os.Open("/.containerconfig")
		if err != nil {
			return err
		}
		defer f.Close()
		var config struct {
			IP string
		}
		if err := json.NewDecoder(f).Decode(&config); err != nil {
			return err
		}
		ip, _, err := net.ParseCIDR(config.IP)
		if err != nil {
			return err
		}
		r.ip = ip.String()
		return nil
	}(); err != nil {
		return err
	}

	r.s3 = s3.New(session.New(&aws.Config{Region: aws.String("us-east-1")}))

	var err error
	if r.rootFS, err = cluster.BuildFlynn(r.bc, args.RootFS, "origin/master", false, os.Stdout); err != nil {
		return fmt.Errorf("could not build flynn: %s", err)
	}
	shutdown.BeforeExit(func() { removeRootFS(r.rootFS) })

	db := postgres.Wait(nil, nil)
	if err := migrations.Migrate(db); err != nil {
		return fmt.Errorf("error migrating db: %s", err)
	}
	db.Close()
	r.db = postgres.Wait(nil, PrepareStatements)
	shutdown.BeforeExit(func() { r.db.Close() })

	for i := 0; i < args.ConcurrentBuilds; i++ {
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
	router.GET("/builds/:build/download", r.downloadBuildLog)
	router.POST("/builds/:build/restart", r.restartBuild)
	router.POST("/builds/:build/explain", r.explainBuild)
	router.GET("/builds", r.getBuilds)
	router.ServeFiles("/assets/*filepath", http.Dir(args.AssetsDir))
	router.GET("/cluster/:cluster", r.clusterAPI(r.getCluster))
	router.POST("/cluster/:cluster", r.clusterAPI(r.addHost))
	router.POST("/cluster/:cluster/release", r.clusterAPI(r.addReleaseHosts))
	router.DELETE("/cluster/:cluster/:host", r.clusterAPI(r.removeHost))

	listenAddr := ":" + os.Getenv("PORT")
	log.Println("Listening on", listenAddr, "...")
	if err := http.ListenAndServe(listenAddr, router); err != nil {
		return fmt.Errorf("error running HTTP server: %s", err)
	}

	return nil
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

export DISCOVERD="http://{{ (index .Cluster.Instances 0).IP }}:1111"

# put the command in a file so the arguments aren't echoed in the logs
cat > /tmp/run-tests.sh <<EOF
build/bin/flynn-host run \
  --host "{{ (index .Cluster.Instances 0).ID }}" \
  --bind "$(pwd):/go/src/github.com/flynn/flynn,/var/run/docker.sock:/var/run/docker.sock,/var/lib/flynn:/var/lib/flynn,/mnt/backups:/mnt/backups" \
  --volume "/tmp" \
  "build/image/test.json" \
  /usr/bin/env \
  ROOT="/go/src/github.com/flynn/flynn" \
  CLUSTER_ADD_ARGS="-p {{ .Config.TLSPin }} default {{ .Cluster.ClusterDomain }} {{ .Config.Key }}" \
  ROUTER_IP="{{ .Cluster.RouterIP }}" \
  DOMAIN="{{ .Cluster.ClusterDomain }}" \
  TEST_RUNNER_AUTH_KEY="${TEST_RUNNER_AUTH_KEY}" \
  BLOBSTORE_S3_CONFIG="${BLOBSTORE_S3_CONFIG}" \
  BLOBSTORE_GCS_CONFIG="${BLOBSTORE_GCS_CONFIG}" \
  BLOBSTORE_AZURE_CONFIG="${BLOBSTORE_AZURE_CONFIG}" \
  /bin/run-flynn-test.sh \
  --cluster-api http://{{ .RunnerIP }}/cluster/{{ .Cluster.ID }} \
  --router-ip {{ .Cluster.RouterIP }} \
  --backups-dir "/mnt/backups" \
  --debug
EOF
chmod +x /tmp/run-tests.sh

timeout --signal=QUIT --kill-after=10 45m /tmp/run-tests.sh

`[1:]))

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

		b.Duration = time.Since(start).String()
		fmt.Fprintf(mainLog, "build finished in %s\n", b.Duration)
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
		} else {
			log.Printf("build %s failed: %s\n", b.ID, err)
			r.updateStatus(b, "failure")
		}
	}()

	log.Printf("building %s\n", b.Commit)

	out := &iotool.SafeWriter{W: io.MultiWriter(os.Stdout, mainLog, &failureBuf)}

	c = cluster.New(r.bc, out)
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
	args := map[string]interface{}{
		"RunnerIP": r.ip,
		"Cluster":  c,
		"Config":   config.Clusters[0],
	}
	if err := testRunScript.Execute(&script, args); err != nil {
		return err
	}
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

	if _, err := file.Seek(0, io.SeekStart); err != nil {
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
		_, err := r.s3.PutObject(&s3.PutObjectInput{
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

func scanBuild(s postgres.Scanner) (*Build, error) {
	var build Build
	if err := s.Scan(
		&build.ID,
		&build.Commit,
		&build.Branch,
		&build.Merge,
		&build.State,
		&build.Version,
		&build.Failures,
		&build.CreatedAt,
		&build.Description,
		&build.LogURL,
		&build.LogFile,
		&build.Duration,
		&build.Reason,
		&build.IssueLink,
	); err != nil {
		return nil, err
	}
	return &build, nil
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
			return
		}
	}

	rows, err := r.db.Query("build_list", count)
	if err != nil {
		httphelper.Error(w, err)
		return
	}
	defer rows.Close()
	var builds []*Build
	for rows.Next() {
		build, err := scanBuild(rows)
		if err != nil {
			httphelper.Error(w, err)
			return
		}
		builds = append(builds, build)
	}
	if err := rows.Err(); err != nil {
		httphelper.Error(w, err)
		return
	}

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
	build, err := scanBuild(r.db.QueryRow("build_select", id))
	if err != nil {
		httphelper.Error(w, err)
		return
	}

	// if it's a V1 build, redirect to the log in S3
	if build.Version == BuildVersion1 {
		http.Redirect(w, req, build.LogURL, http.StatusMovedPermanently)
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
		if err := tpl.Execute(w, build); err != nil {
			log.Printf("error executing build-log template: %s", err)
		}
		return
	}

	// serve the build log as either an SSE or plain text stream
	ch := make(chan string)
	stream, err := getBuildLogStream(build, ch)
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
	build, err := scanBuild(r.db.QueryRow("build_select", id))
	if err != nil {
		httphelper.Error(w, err)
		return
	}

	res, err := http.Get(build.LogURL)
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
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=\"flynn-ci-build-%s\"", path.Base(build.LogURL)))
	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, "MIME-Version: 1.0\r\nContent-Type: multipart/mixed; boundary=%q\r\n\r\n", params["boundary"])
	io.Copy(w, res.Body)
}

func (r *Runner) restartBuild(w http.ResponseWriter, req *http.Request, ps httprouter.Params) {
	id := ps.ByName("build")
	build, err := scanBuild(r.db.QueryRow("build_select", id))
	if err != nil {
		httphelper.Error(w, err)
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
	build, err := scanBuild(r.db.QueryRow("build_select", id))
	if err != nil {
		httphelper.Error(w, err)
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
	if e, ok := event.(*PullRequestEvent); ok && e.Action != "opened" && e.Action != "synchronize" {
		return false
	}
	if e, ok := event.(*PushEvent); ok && (e.Deleted || e.Ref != "refs/heads/master") {
		return false
	}
	return !strings.Contains(strings.ToLower(event.String()), "[ci skip]")
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

func (r *Runner) buildPending() error {
	rows, err := r.db.Query("build_pending")
	if err != nil {
		return err
	}
	defer rows.Close()
	var pending []*Build
	for rows.Next() {
		build, err := scanBuild(rows)
		if err != nil {
			return err
		}
		pending = append(pending, build)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for _, b := range pending {
		go r.build(b)
	}
	return nil
}

func (r *Runner) save(b *Build) error {
	return r.db.Exec("build_insert",
		b.ID,
		b.Commit,
		b.Branch,
		b.Merge,
		b.State,
		b.Version,
		b.Failures,
		b.CreatedAt,
		b.Description,
		b.LogURL,
		b.LogFile,
		b.Duration,
		b.Reason,
		b.IssueLink,
	)
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
