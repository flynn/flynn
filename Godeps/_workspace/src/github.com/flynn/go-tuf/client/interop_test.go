package client

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/agl/ed25519"
	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/flynn/go-tuf/data"
	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/flynn/go-tuf/util"
	"github.com/flynn/go-tuf"
	. "gopkg.in/check.v1"
)

type InteropSuite struct{}

var _ = Suite(&InteropSuite{})

var pythonTargets = map[string][]byte{
	"/file1.txt":     []byte("file1.txt"),
	"/dir/file2.txt": []byte("file2.txt"),
}

func (InteropSuite) TestGoClientPythonGenerated(c *C) {
	// start file server
	cwd, err := os.Getwd()
	c.Assert(err, IsNil)
	testDataDir := filepath.Join(cwd, "testdata")
	addr, cleanup := startFileServer(c, testDataDir)
	defer cleanup()

	for _, dir := range []string{"with-consistent-snapshot", "without-consistent-snapshot"} {
		remote, err := HTTPRemoteStore(
			fmt.Sprintf("http://%s/%s/repository", addr, dir),
			&HTTPRemoteOptions{MetadataPath: "metadata", TargetsPath: "targets"},
		)
		c.Assert(err, IsNil)

		// initiate a client with the root keys
		f, err := os.Open(filepath.Join("testdata", dir, "keystore", "root_key.pub"))
		c.Assert(err, IsNil)
		key := &data.Key{}
		c.Assert(json.NewDecoder(f).Decode(key), IsNil)
		c.Assert(key.Type, Equals, "ed25519")
		c.Assert(key.Value.Public, HasLen, ed25519.PublicKeySize)
		client := NewClient(MemoryLocalStore(), remote)
		c.Assert(client.Init([]*data.Key{key}, 1), IsNil)

		// check update returns the correct updated targets
		files, err := client.Update()
		c.Assert(err, IsNil)
		c.Assert(files, HasLen, len(pythonTargets))
		for name, data := range pythonTargets {
			file, ok := files[name]
			if !ok {
				c.Fatalf("expected updated targets to contain %s", name)
			}
			meta, err := util.GenerateFileMeta(bytes.NewReader(data), file.HashAlgorithms()...)
			c.Assert(err, IsNil)
			c.Assert(util.FileMetaEqual(file, meta), IsNil)
		}

		// download the files and check they have the correct content
		for name, data := range pythonTargets {
			var dest testDestination
			c.Assert(client.Download(name, &dest), IsNil)
			c.Assert(dest.deleted, Equals, false)
			c.Assert(dest.String(), Equals, string(data))
		}
	}
}

func generateRepoFS(c *C, dir string, files map[string][]byte, consistentSnapshot bool) *tuf.Repo {
	repo, err := tuf.NewRepo(tuf.FileSystemStore(dir, nil))
	c.Assert(err, IsNil)
	if !consistentSnapshot {
		c.Assert(repo.Init(false), IsNil)
	}
	for _, role := range []string{"root", "snapshot", "targets", "timestamp"} {
		_, err := repo.GenKey(role)
		c.Assert(err, IsNil)
	}
	for file, data := range files {
		path := filepath.Join(dir, "staged", "targets", file)
		c.Assert(os.MkdirAll(filepath.Dir(path), 0755), IsNil)
		c.Assert(ioutil.WriteFile(path, data, 0644), IsNil)
		c.Assert(repo.AddTarget(file, nil), IsNil)
	}
	c.Assert(repo.Snapshot(tuf.CompressionTypeNone), IsNil)
	c.Assert(repo.Timestamp(), IsNil)
	c.Assert(repo.Commit(), IsNil)
	return repo
}

func (InteropSuite) TestPythonClientGoGenerated(c *C) {
	// clone the Python client if necessary
	cwd, err := os.Getwd()
	c.Assert(err, IsNil)
	tufDir := filepath.Join(cwd, "testdata", "tuf")
	if _, err := os.Stat(tufDir); os.IsNotExist(err) {
		c.Assert(exec.Command(
			"git",
			"clone",
			"--quiet",
			"--branch=v0.9.9",
			"--depth=1",
			"https://github.com/theupdateframework/tuf.git",
			tufDir,
		).Run(), IsNil)
	}

	tmp := c.MkDir()
	files := map[string][]byte{
		"foo.txt":     []byte("foo"),
		"bar/baz.txt": []byte("baz"),
	}

	// start file server
	addr, cleanup := startFileServer(c, tmp)
	defer cleanup()

	// setup Python env
	environ := os.Environ()
	pythonEnv := make([]string, 0, len(environ)+1)
	// remove any existing PYTHONPATH from the environment
	for _, e := range environ {
		if strings.HasPrefix(e, "PYTHONPATH=") {
			continue
		}
		pythonEnv = append(pythonEnv, e)
	}
	pythonEnv = append(pythonEnv, "PYTHONPATH="+tufDir)

	for _, consistentSnapshot := range []bool{false, true} {
		// generate repository
		name := fmt.Sprintf("consistent-snapshot-%t", consistentSnapshot)
		dir := filepath.Join(tmp, name)
		generateRepoFS(c, dir, files, consistentSnapshot)

		// create initial files for Python client
		clientDir := filepath.Join(dir, "client")
		currDir := filepath.Join(clientDir, "metadata", "current")
		prevDir := filepath.Join(clientDir, "metadata", "previous")
		c.Assert(os.MkdirAll(currDir, 0755), IsNil)
		c.Assert(os.MkdirAll(prevDir, 0755), IsNil)
		rootJSON, err := ioutil.ReadFile(filepath.Join(dir, "repository", "root.json"))
		c.Assert(err, IsNil)
		c.Assert(ioutil.WriteFile(filepath.Join(currDir, "root.json"), rootJSON, 0644), IsNil)

		// run Python client update
		cmd := exec.Command("python", filepath.Join(cwd, "testdata", "client.py"), "--repo=http://"+addr+"/"+name)
		cmd.Env = pythonEnv
		cmd.Dir = clientDir
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		c.Assert(cmd.Run(), IsNil)

		// check the target files got downloaded
		for path, expected := range files {
			actual, err := ioutil.ReadFile(filepath.Join(clientDir, "targets", path))
			c.Assert(err, IsNil)
			c.Assert(actual, DeepEquals, expected)
		}
	}
}

func startFileServer(c *C, dir string) (string, func() error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	c.Assert(err, IsNil)
	addr := l.Addr().String()
	go http.Serve(l, http.FileServer(http.Dir(dir)))
	return addr, l.Close
}
