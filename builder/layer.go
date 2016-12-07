package main

import (
	"bufio"
	"crypto/sha512"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"net"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"github.com/docker/go-units"
	"github.com/flynn/flynn/builder/store"
	ct "github.com/flynn/flynn/controller/types"
	"github.com/flynn/flynn/host/resource"
	"github.com/flynn/flynn/host/types"
	"github.com/flynn/flynn/pkg/cluster"
	"github.com/flynn/flynn/pkg/exec"
	"github.com/flynn/go-docopt"
	"github.com/rminnich/go9p"
	"gopkg.in/inconshreveable/log15.v2"
)

const (
	LocalStoreDir = "/var/lib/flynn/local"

	// use a larger default temp disk size as most build jobs need
	// more than 100MB
	DefaultTempDiskSize int64 = 1 * units.GiB
)

var cmdLayer = Command{
	Run: runLayer,
	Usage: `
usage: flynn-build layer [options] <base> <script> [<inputs>...]

Options:
  --limits=<limits>   resource limits

Build an image layer on top of <base> using <script>,
serializing the layer JSON to STDOUT.

A hash of the inputs is generated to determine if the layer has already been
built (which prevents changes to the builder code rebuilding all images).

Expects flynn-host to be running locally at 192.0.2.100 (which
is started by host/start-build-daemon.sh).
`[1:],
}

func runLayer(args *docopt.Args) error {
	log := log15.New("component", "build")
	log.SetHandler(log15.StreamHandler(os.Stderr, log15.LogfmtFormat()))

	// use a local store
	store, err := store.NewLocalStore(LocalStoreDir)
	if err != nil {
		log.Error("error initializing local store", "err", err)
		return err
	}

	// load the base artifact
	baseArtifact, err := loadArtifact(args.String["<base>"])
	if err != nil {
		log.Error("error loading base artifact", "err", err)
		return err
	}

	// check if we already created a layer for the given name + base + script + inputs
	cwd, err := os.Getwd()
	if err != nil {
		log.Error("error getting current directory", "err", err)
		return err
	}
	name := filepath.Base(cwd)
	script := args.String["<script>"]
	inputs := append(args.All["<inputs>"].([]string), script)
	hash, err := generateHash(name, baseArtifact, inputs...)
	if err != nil {
		log.Error("error generating hash of the inputs", "err", err)
		return err
	}
	layer, ok := store.GetLayer(hash)
	if ok {
		log.Info("using cached layer", "name", name, "script", script, "hash", hash)
		return json.NewEncoder(os.Stdout).Encode(layer)
	}

	// load the builder artifact
	builderArtifact, err := loadArtifact("builder")
	if err != nil {
		log.Error("error loading builder artifact", "err", err)
		return err
	}

	// run the command with the base artifact layered on top of the
	// builder artifact so we can wrap the command in a call to
	// 'flynn-build run' (which outputs the changes to stdout)
	cmd := exec.CommandUsingArtifacts(
		[]*ct.Artifact{builderArtifact, baseArtifact},
		"/bin/flynn-build", "run", "bash", "-ex", "/src/"+script,
	)

	// set the resources
	cmd.Resources = resource.WithLimit(resource.TypeTempDisk, DefaultTempDiskSize)
	if limits := args.String["--limits"]; limits != "" {
		resources, err := resource.ParseCSV(limits)
		if err != nil {
			return fmt.Errorf("error parsing --limits: %s", err)
		}
		for typ, limit := range resources {
			cmd.Resources[typ] = limit
		}
	}

	// create a directory containing the inputs so we can ensure the job
	// only accesses declared inputs (thus enforcing the correctness of
	// the input hash generated above)
	srcDir, err := ioutil.TempDir("", "flynn-build-src")
	if err != nil {
		log.Error("error creating temp dir", "err", err)
		return err
	}
	defer os.RemoveAll(srcDir)
	copyInput := func(input string) error {
		src, err := os.Open(input)
		if err != nil {
			return err
		}
		defer src.Close()
		stat, err := src.Stat()
		if err != nil {
			return err
		}
		path := filepath.Join(srcDir, input)
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			return err
		}
		dst, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, stat.Mode())
		if err != nil {
			return err
		}
		_, err = io.Copy(dst, src)
		return err
	}
	for _, input := range inputs {
		if err := copyInput(input); err != nil {
			log.Error("error copying input", "path", input, "err", err)
			return err
		}
	}

	// mount the source directory at /src and a temp dir at /out as 9p
	// filesystems, listening on the local flynn-host's network
	outDir, err := ioutil.TempDir("", "flynn-build-out")
	if err != nil {
		log.Error("error creating temp dir", "err", err)
		return err
	}
	defer os.RemoveAll(outDir)
	srcFS, err := add9pfs(cmd, srcDir, "/src", false)
	if err != nil {
		log.Error("error starting 9p filesystem", "err", err)
		return err
	}
	defer srcFS.Close()
	outFS, err := add9pfs(cmd, outDir, "/out", true)
	if err != nil {
		log.Error("error starting 9p filesystem", "err", err)
		return err
	}
	defer outFS.Close()

	// run the job using the local flynn-host
	cmd.Host = cluster.NewHost("build", "192.0.2.100:1113", nil, nil)
	cmd.HostNetwork = true

	// prefix output with the name and script
	logR, logW := io.Pipe()
	defer logR.Close()
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		log.Error("error creating stdout pipe", "err", err)
		return err
	}
	go io.Copy(logW, stdout)
	stderr, err := cmd.StderrPipe()
	if err != nil {
		log.Error("error creating stderr pipe", "err", err)
		return err
	}
	go io.Copy(logW, stderr)
	go func() {
		s := bufio.NewScanner(logR)
		for s.Scan() {
			fmt.Fprintf(
				os.Stderr,
				"%s: %s:%s: %s\n",
				time.Now().Format("15:04:05.999"),
				name, filepath.Base(script), s.Text(),
			)
		}
	}()

	// give the job some common permissions for installing software
	cmd.LinuxCapabilities = append(host.DefaultCapabilities, "CAP_AUDIT_WRITE")

	// run the job
	log.Info("running build job", "name", name, "script", script, "hash", hash)
	if err := cmd.Run(); err != nil {
		log.Error("error running the build job", "err", err)
		return err
	}

	// copy the layer to the store
	f, err := os.Open(filepath.Join(outDir, "layer.squashfs"))
	if err != nil {
		log.Error("error opening squashfs layer", "err", err)
		return err
	}
	defer f.Close()
	meta := map[string]string{
		"flynn-build.name":   name,
		"flynn-build.script": script,
	}
	layer, err = store.PutLayer(hash, f, meta)
	if err != nil {
		log.Error("error copying the layer to the store", "err", err)
		return err
	}

	// write the layer to STDOUT
	return json.NewEncoder(os.Stdout).Encode(layer)
}

// loadArtifact loads an artifact from ROOT/image/${name}.json, finding it
// relative to the current binary's path ROOT/builder/bin/flynn-build
func loadArtifact(name string) (*ct.Artifact, error) {
	selfPath, err := filepath.Abs(os.Args[0])
	if err != nil {
		return nil, err
	}
	binDir := filepath.Dir(selfPath)

	f, err := os.Open(filepath.Join(binDir, "..", "..", "image", name+".json"))
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var artifact ct.Artifact
	return &artifact, json.NewDecoder(f).Decode(&artifact)
}

func generateHash(name string, artifact *ct.Artifact, inputs ...string) (string, error) {
	h := sha512.New512_256()
	h.Write([]byte(name))
	h.Write(artifact.RawManifest)
	for _, input := range inputs {
		f, err := os.Open(input)
		if err != nil {
			return "", err
		}
		defer f.Close()
		stat, err := f.Stat()
		if err != nil {
			return "", err
		}
		fmt.Fprintf(h, "%s%d", input, stat.Size())
		if _, err := io.Copy(h, f); err != nil {
			return "", err
		}
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func add9pfs(cmd *exec.Cmd, root, location string, writeable bool) (net.Listener, error) {
	l, err := net.Listen("tcp", "192.0.2.100:0")
	if err != nil {
		return nil, err
	}
	fs := &go9p.Ufs{Root: root}
	fs.Dotu = true
	fs.Start(fs)
	go fs.StartListener(l)
	addr := l.Addr().(*net.TCPAddr)
	mount := host.Mount{
		Device:   "9p",
		Location: location,
		Target:   addr.IP.String(),
		Data:     fmt.Sprintf("trans=tcp,port=%d", addr.Port),
	}
	if !writeable {
		mount.Flags = syscall.MS_RDONLY
	}
	cmd.Mounts = append(cmd.Mounts, mount)
	return l, nil
}
