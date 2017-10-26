/*
gitreceive handles 'smart' Git HTTP requests for Flynn

This HTTP server can service 'git clone', 'git push' etc. commands
from Git clients that use the 'smart' Git HTTP protocol (git-upload-pack
and git-receive-pack).

Derived from https://gitlab.com/gitlab-org/gitlab-git-http-server
*/
package main

import (
	"archive/tar"
	"compress/gzip"
	"crypto/hmac"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/flynn/flynn/controller/client"
	"github.com/flynn/flynn/controller/utils"
	"github.com/flynn/flynn/pkg/archiver"
	"github.com/flynn/flynn/pkg/ctxhelper"
	"github.com/flynn/flynn/pkg/httphelper"
	"github.com/flynn/flynn/pkg/status"
)

func main() {
	key := os.Getenv("CONTROLLER_KEY")
	if key == "" {
		log.Fatal("missing CONTROLLER_KEY env var")
	}
	cc, err := controller.NewClient("", key)
	if err != nil {
		log.Fatalln("Unable to connect to controller:", err)
	}
	log.Fatal(http.ListenAndServe(":"+os.Getenv("PORT"), httphelper.ContextInjector("gitreceive", httphelper.NewRequestLogger(newGitHandler(cc, []byte(key))))))
}

type gitHandler struct {
	controller controller.Client
	authKey    []byte
}

type gitService struct {
	method     string
	suffix     string
	handleFunc func(gitEnv, string, string, http.ResponseWriter, *http.Request) bool
	rpc        string
}

type gitEnv struct {
	App string
}

// Routing table
var gitServices = [...]gitService{
	{"GET", "/info/refs", handleGetInfoRefs, ""},
	{"POST", "/git-upload-pack", handlePostRPC, "git-upload-pack"},
	{"POST", "/git-receive-pack", handlePostRPC, "git-receive-pack"},
}

func newGitHandler(controller controller.Client, authKey []byte) *gitHandler {
	return &gitHandler{controller, authKey}
}

func (h *gitHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var g gitService

	if r.URL.Path == status.Path {
		status.HealthyHandler.ServeHTTP(w, r)
		return
	}

	// Look for a matching Git service
	foundService := false
	for _, g = range gitServices {
		if r.Method == g.method && strings.HasSuffix(r.URL.Path, g.suffix) {
			foundService = true
			break
		}
	}
	name := strings.TrimSuffix(strings.TrimPrefix(strings.TrimSuffix(r.URL.Path, g.suffix), "/"), ".git")
	if !foundService || !utils.AppNamePattern.MatchString(name) {
		// The protocol spec in git/Documentation/technical/http-protocol.txt
		// says we must return 403 if no matching service is found.
		http.Error(w, "Forbidden", 403)
		return
	}

	_, password, _ := r.BasicAuth()
	if !hmac.Equal([]byte(password), h.authKey) {
		w.Header().Set("WWW-Authenticate", "Basic")
		http.Error(w, "Authentication required", 401)
		return
	}

	// Lookup app
	app, err := h.controller.GetApp(name)
	if err == controller.ErrNotFound {
		http.Error(w, "unknown app", 404)
		return
	} else if err != nil {
		fail500(w, "getApp", err)
		return
	}

	repoPath, err := prepareRepo(app.ID)
	if err != nil {
		fail500(w, "prepareRepo", err)
		return
	}
	defer os.RemoveAll(repoPath)

	success := g.handleFunc(gitEnv{App: app.ID}, g.rpc, repoPath, w, r)
	if success && g.rpc == "git-receive-pack" {
		if err := uploadRepo(repoPath, app.ID); err != nil {
			logError(w, "uploadRepo", err)
		}
	}
}

func handleGetInfoRefs(env gitEnv, _ string, path string, w http.ResponseWriter, r *http.Request) bool {
	rpc := r.URL.Query().Get("service")
	if !(rpc == "git-upload-pack" || rpc == "git-receive-pack") {
		// The 'dumb' Git HTTP protocol is not supported
		http.Error(w, "Not Found", 404)
		return false
	}

	// Prepare our Git subprocess
	cmd, pipe := gitCommand(env, "git", subCommand(rpc), "--stateless-rpc", "--advertise-refs", path)
	if err := cmd.Start(); err != nil {
		fail500(w, "handleGetInfoRefs", err)
		return false
	}
	defer cleanUpProcessGroup(cmd) // Ensure brute force subprocess clean-up

	// Start writing the response
	w.Header().Add("Content-Type", fmt.Sprintf("application/x-%s-advertisement", rpc))
	w.Header().Add("Cache-Control", "no-cache")
	w.WriteHeader(200) // Don't bother with HTTP 500 from this point on, just return
	if err := pktLine(w, fmt.Sprintf("# service=%s\n", rpc)); err != nil {
		logError(w, "handleGetInfoRefs response", err)
		return false
	}
	if err := pktFlush(w); err != nil {
		logError(w, "handleGetInfoRefs response", err)
		return false
	}
	if _, err := io.Copy(w, pipe); err != nil {
		logError(w, "handleGetInfoRefs read from subprocess", err)
		return false
	}
	if err := cmd.Wait(); err != nil {
		logError(w, "handleGetInfoRefs wait for subprocess", err)
		return false
	}

	return true
}

func handlePostRPC(env gitEnv, rpc string, path string, w http.ResponseWriter, r *http.Request) bool {

	// The client request body may have been gzipped.
	body := r.Body
	if r.Header.Get("Content-Encoding") == "gzip" {
		var err error
		body, err = gzip.NewReader(r.Body)
		if err != nil {
			fail500(w, "handlePostRPC", err)
			return false
		}
	}

	// Prepare our Git subprocess
	cmd, pipe := gitCommand(env, "git", subCommand(rpc), "--stateless-rpc", path)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		fail500(w, "handlePostRPC", err)
		return false
	}
	defer stdin.Close()
	if err := cmd.Start(); err != nil {
		fail500(w, "handlePostRPC", err)
		return false
	}
	go func(done <-chan bool) {
		<-done
		cleanUpProcessGroup(cmd) // Ensure brute force subprocess clean-up
	}(w.(http.CloseNotifier).CloseNotify())

	// Write the client request body to Git's standard input
	if _, err := io.Copy(stdin, body); err != nil {
		fail500(w, "handlePostRPC write to subprocess", err)
		return false
	}

	// Start writing the response
	w.Header().Add("Content-Type", fmt.Sprintf("application/x-%s-result", rpc))
	w.Header().Add("Cache-Control", "no-cache")
	w.WriteHeader(200) // Don't bother with HTTP 500 from this point on, just return
	if _, err := io.Copy(newWriteFlusher(w), pipe); err != nil {
		logError(w, "handlePostRPC read from subprocess", err)
		return false
	}
	if err := cmd.Wait(); err != nil {
		logError(w, "handlePostRPC wait for subprocess", err)
		return false
	}

	return true
}

func fail500(w http.ResponseWriter, context string, err error) {
	http.Error(w, "Internal server error", 500)
	logError(w, context, err)
}

func logError(w http.ResponseWriter, msg string, err error) {
	logger, _ := ctxhelper.LoggerFromContext(w.(*httphelper.ResponseWriter).Context())
	logger.Error(msg, "error", err)
}

// Git subprocess helpers
func subCommand(rpc string) string {
	return strings.TrimPrefix(rpc, "git-")
}

func gitCommand(env gitEnv, name string, args ...string) (*exec.Cmd, io.Reader) {
	cmd := exec.Command(name, args...)
	// Start the command in its own process group (nice for signalling)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	// Explicitly set the environment for the Git command
	cmd.Env = append(os.Environ(),
		fmt.Sprintf("RECEIVE_APP=%s", env.App),
	)

	r, _ := cmd.StdoutPipe()
	cmd.Stderr = cmd.Stdout

	return cmd, r
}

func cleanUpProcessGroup(cmd *exec.Cmd) {
	if cmd == nil {
		return
	}

	process := cmd.Process
	if process != nil && process.Pid > 0 {
		// Send SIGTERM to the process group of cmd
		syscall.Kill(-process.Pid, syscall.SIGTERM)
	}

	// reap our child process
	go cmd.Wait()
}

// Git HTTP line protocol functions
func pktLine(w io.Writer, s string) error {
	_, err := fmt.Fprintf(w, "%04x%s", len(s)+4, s)
	return err
}

func pktFlush(w io.Writer) error {
	_, err := fmt.Fprint(w, "0000")
	return err
}

func newWriteFlusher(w http.ResponseWriter) io.Writer {
	return writeFlusher{w.(interface {
		io.Writer
		http.Flusher
	})}
}

type writeFlusher struct {
	wf interface {
		io.Writer
		http.Flusher
	}
}

func (w writeFlusher) Write(p []byte) (int, error) {
	defer w.wf.Flush()
	return w.wf.Write(p)
}

var prereceiveHook = []byte(`#!/bin/bash
set -eo pipefail;

git-archive-all() {
	GIT_DIR="$(pwd)"
	cd ..
	git checkout --force --quiet $1
	git submodule --quiet update --force --init --checkout --recursive
	tar --create --exclude-vcs .
}

while read oldrev newrev refname; do
	if [[ $refname = "refs/heads/master" ]]; then
		git-archive-all $newrev | /bin/flynn-receiver "$RECEIVE_APP" "$newrev" --meta git=true --meta "git.commit=$newrev"| sed -u "s/^/"$'\e[1G\e[K'"/"
		master_pushed=1
		break
	fi
done

if [[ -z "${master_pushed}" ]]; then
  echo "The push must include a change to the master branch to be deployed."
  exit 1
fi
`)

func blobstoreCacheURL(cacheKey string) string {
	return fmt.Sprintf("http://blobstore.discoverd/repos/%s.tar", cacheKey)
}

func prepareRepo(cacheKey string) (string, error) {
	path, err := ioutil.TempDir("", "repo-"+cacheKey)
	if err != nil {
		return "", err
	}

	res, err := http.Get(blobstoreCacheURL(cacheKey))
	if err != nil {
		return "", err
	}
	defer res.Body.Close()

	if res.StatusCode == 404 {
		return path, initRepo(path)
	}
	if res.StatusCode != 200 {
		return "", fmt.Errorf("unexpected error %d retrieving cached repo", res.StatusCode)
	}

	if err := archiver.Untar(path, tar.NewReader(res.Body)); err != nil {
		return "", err
	}
	if err := writeRepoHook(path); err != nil {
		return "", err
	}

	return path, nil
}

func initRepo(path string) error {
	cmd := exec.Command("git", "init")
	cmd.Dir = path
	if err := cmd.Run(); err != nil {
		return err
	}
	return writeRepoHook(path)
}

func writeRepoHook(path string) error {
	return ioutil.WriteFile(filepath.Join(path, ".git", "hooks", "pre-receive"), prereceiveHook, 0755)
}

func uploadRepo(path, cacheKey string) error {
	r, w := io.Pipe()
	tw := tar.NewWriter(w)

	errCh := make(chan error)
	go func() {
		err := archiver.Tar(path, tw, func(n string) bool { return strings.HasSuffix(n, ".git") || strings.Contains(n, ".git/") })
		tw.Close()
		w.Close()
		errCh <- err
	}()

	// upload the tarball to the blobstore
	req, _ := http.NewRequest("PUT", blobstoreCacheURL(cacheKey), r)
	resp, err := http.DefaultClient.Do(req)
	if err := <-errCh; err != nil {
		return err
	}
	resp.Body.Close()
	return err
}
