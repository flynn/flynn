package main

import (
	"bufio"
	"bytes"
	"crypto/sha512"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"go/build"
	"io"
	"io/ioutil"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"text/template"
	"time"

	"github.com/cheggaaa/pb"
	"github.com/docker/docker/pkg/term"
	"github.com/docker/go-units"
	ct "github.com/flynn/flynn/controller/types"
	"github.com/flynn/flynn/controller/utils"
	"github.com/flynn/flynn/host/resource"
	"github.com/flynn/flynn/host/types"
	"github.com/flynn/flynn/pkg/exec"
	"github.com/flynn/flynn/pkg/tufutil"
	"github.com/flynn/flynn/pkg/version"
	"github.com/flynn/go-docopt"
	tuf "github.com/flynn/go-tuf/client"
	tufdata "github.com/flynn/go-tuf/data"
	"github.com/golang/groupcache/singleflight"
	"github.com/rminnich/go9p"
	"github.com/tent/canonical-json-go"
	"gopkg.in/inconshreveable/log15.v2"
)

var cmdBuild = Command{
	Run: runBuild,
	Usage: `
usage: flynn-builder build [options]

options:
  -x, --version=<version>   version to use [default: dev]
  -t, --tuf-db=<path>       path to TUF database [default: build/tuf.db]
  -v, --verbose             be verbose

Build Flynn images using builder/manifest.json.
`[1:],
}

type Builder struct {
	// baseLayer is used when building an image which has no
	// dependencies (e.g. the ubuntu-trusty image)
	baseLayer *host.Mountspec

	// artifacts is a map of built artifacts and is written to
	// build/images.json on success
	artifacts    map[string]*ct.Artifact
	artifactsMtx sync.RWMutex

	// envTemplateData is used as the data when interpolating
	// environment variable templates
	envTemplateData map[string]string

	// goInputs is a set of Go input loaders per platform
	goInputs    map[GoPlatform]*GoInputs
	goInputsMtx sync.Mutex

	// tufConfig is the TUF config from the manifest
	tufConfig *TUFConfig
	tufClient *tuf.Client

	log     log15.Logger
	version string
	bar     *pb.ProgressBar
}

type Manifest struct {
	TUFConfig *TUFConfig        `json:"tuf,omitempty"`
	BaseLayer *host.Mountspec   `json:"base_layer,omitempty"`
	Images    []*Image          `json:"images,omitempty"`
	Templates map[string]*Image `json:"templates,omitempty"`
	Manifests map[string]string `json:"manifests,omitempty"`
}

type TUFConfig struct {
	Repository string         `json:"repository,omitempty"`
	RootKeys   []*tufdata.Key `json:"root_keys,omitempty"`
}

type Image struct {
	ID         string              `json:"id,omitempty"`
	Base       string              `json:"base,omitempty"`
	Template   string              `json:"template,omitempty"`
	Env        map[string]string   `json:"env,omitempty"`
	Layers     []*Layer            `json:"layers,omitempty"`
	Entrypoint *ct.ImageEntrypoint `json:"entrypoint,omitempty"`
}

type Layer struct {
	// Name is the name of the layer used in log output (defaults to the
	// image's ID)
	Name string `json:"name,omitempty"`

	// BuildWith overrides the image used to build the layer (which
	// defaults to the image's base image), thus supports using tools
	// to build a layer but not putting those tools in the final image
	BuildWith string `json:"build_with,omitempty"`

	// Inputs is a list of files required to build the layer, each of them
	// being hashed to generate the resulting layer ID
	Inputs []string `json:"inputs,omitempty"`

	// Run is a list of commands to be run to build the layer, each of them
	// being hashed to generate the resulting layer ID
	Run []string `json:"run,omitempty"`

	// Script is added as an input and run with 'bash -e'
	Script string `json:"script,omitempty"`

	// GoBuild is a set of directories to build using 'go build'.
	//
	// The go/build package is used to load the required source files which
	// are then added as inputs.
	GoBuild map[string]string `json:"gobuild,omitempty"`

	// CGoBuild is a set of directories to build with cgo enabled.
	//
	// The go/build package is used to load the required source files which
	// are then added as inputs.
	CGoBuild map[string]string `json:"cgobuild,omitempty"`

	// Copy is a set of inputs to copy into the layer
	Copy map[string]string `json:"copy,omitempty"`

	// Env is a set of environment variables to set when building the
	// layer, each of them being hashed to generate the resulting layer ID
	Env map[string]string `json:"env,omitempty"`

	// Limits is a set of limits to set on the build job
	Limits map[string]string `json:"limits,omitempty"`

	// LinuxCapabilities is a list of extra capabilities to set
	LinuxCapabilities []string `json:"linux_capabilities,omitempty"`
}

// Build is used to track an image build
type Build struct {
	Image *Image

	// Err is set if the build fails, in which case all dependent builds
	// are aborted
	Err error

	// Once is used to ensure the build is only started once
	Once sync.Once

	// StartedAt is the time the build started
	StartedAt time.Time

	// Abort is set if a dependency fails to build
	Abort bool

	// Dependencies is a list of builds which this build depends on
	Dependencies map[*Build]struct{}

	// Dependents is a list of builds which depend on this build
	Dependents map[*Build]struct{}
}

func NewBuild(image *Image) *Build {
	return &Build{
		Image:        image,
		Dependencies: make(map[*Build]struct{}),
		Dependents:   make(map[*Build]struct{}),
	}
}

func (b *Build) AddDependency(dep *Build) {
	b.Dependencies[dep] = struct{}{}
}

func (b *Build) RemoveDependency(dep *Build) {
	delete(b.Dependencies, dep)
}

func (b *Build) AddDependent(dep *Build) {
	b.Dependents[dep] = struct{}{}
}

func runBuild(args *docopt.Args) error {
	tty := term.IsTerminal(os.Stderr.Fd())

	manifest, err := loadManifest()
	if err != nil {
		return err
	} else if len(manifest.Images) == 0 {
		return errors.New("no images to build")
	}

	bar, err := NewProgressBar(len(manifest.Images), tty)
	if err != nil {
		return err
	}
	bar.Start()
	defer bar.Finish()

	debugLog := fmt.Sprintf("build/log/build-%d.log", time.Now().UnixNano())
	if err := os.MkdirAll(filepath.Dir(debugLog), 0755); err != nil {
		return err
	}
	log := newLogger(tty, debugLog, args.Bool["--verbose"])

	log.Info("building Flynn", "version", args.String["--version"], "log", debugLog)

	log.Info("initialising TUF client", "db", args.String["--tuf-db"])
	tufClient, err := newTUFClient(manifest.TUFConfig, args.String["--tuf-db"])
	if err != nil {
		return err
	}

	for _, image := range manifest.Images {
		if image.Template != "" {
			t, ok := manifest.Templates[image.Template]
			if !ok {
				return fmt.Errorf("unknown template %q", image.Template)
			}
			image.Layers = t.Layers
		}
	}

	tufRootKeys, _ := json.Marshal(manifest.TUFConfig.RootKeys)
	builder := &Builder{
		tufClient: tufClient,
		tufConfig: manifest.TUFConfig,
		baseLayer: manifest.BaseLayer,
		artifacts: make(map[string]*ct.Artifact),
		envTemplateData: map[string]string{
			"TUFRootKeys":   string(tufRootKeys),
			"TUFRepository": manifest.TUFConfig.Repository,
		},
		goInputs: make(map[GoPlatform]*GoInputs),
		log:      log,
		version:  args.String["--version"],
		bar:      bar,
	}

	log.Info("building images")
	if err := builder.Build(manifest.Images); err != nil {
		return err
	}

	log.Info("writing manifests")
	if err := builder.WriteManifests(manifest.Manifests, manifest.TUFConfig.Repository); err != nil {
		return err
	}

	log.Info("writing images")
	return builder.WriteImages()
}

func newTUFClient(config *TUFConfig, dbPath string) (*tuf.Client, error) {
	local, err := tuf.FileLocalStore(dbPath)
	if err != nil {
		return nil, fmt.Errorf("error creating local TUF client: %s", err)
	}
	opts := &tuf.HTTPRemoteOptions{
		UserAgent: fmt.Sprintf("flynn-builder/%s", version.String()),
		Retries:   tufutil.DefaultHTTPRetries,
	}
	remote, err := tuf.HTTPRemoteStore(config.Repository, opts)
	if err != nil {
		return nil, fmt.Errorf("error creating remote TUF client: %s", err)
	}
	client := tuf.NewClient(local, remote)
	_, err = client.Update()
	if err == nil || tuf.IsLatestSnapshot(err) {
		return client, nil
	}
	if err == tuf.ErrNoRootKeys {
		if err := client.Init(config.RootKeys, len(config.RootKeys)); err != nil {
			return nil, err
		}
		_, err = client.Update()
	}
	return client, err
}

// newLogger returns a log15.Logger which writes to stdout and a log file
func newLogger(tty bool, file string, verbose bool) log15.Logger {
	stdoutFormat := log15.LogfmtFormat()
	if tty {
		stdoutFormat = log15.TerminalFormat()
	}
	stdoutHandler := log15.StreamHandler(os.Stdout, stdoutFormat)
	if !verbose {
		stdoutHandler = log15.LvlFilterHandler(log15.LvlInfo, stdoutHandler)
	}
	log := log15.New()
	log.SetHandler(log15.MultiHandler(
		log15.Must.FileHandler(file, log15.LogfmtFormat()),
		stdoutHandler,
	))
	return log
}

func loadManifest() (*Manifest, error) {
	f, err := os.Open("builder/manifest.json")
	if err != nil {
		return nil, err
	}
	defer f.Close()
	manifest := &Manifest{}
	return manifest, json.NewDecoder(f).Decode(manifest)
}

// Build builds a list of images, ensuring that each image is built after any
// dependent images have been built
func (b *Builder) Build(images []*Image) error {
	builds := make(map[string]*Build, len(images))
	for _, image := range images {
		builds[image.ID] = NewBuild(image)
	}

	addDependency := func(build *Build, dependsOn string) error {
		dep, ok := builds[dependsOn]
		if !ok {
			return fmt.Errorf("unknown image dependency: %s -> %s", build.Image.ID, dependsOn)
		}
		build.AddDependency(dep)
		dep.AddDependent(build)
		return nil
	}

	for _, build := range builds {
		image := build.Image

		// determine build dependencies
		// TODO: check for circular dependencies
		if image.Base != "" {
			addDependency(build, image.Base)
		}
		for _, l := range image.Layers {
			// build Go binaries using the Go image
			if l.BuildWith == "" && (len(l.GoBuild) > 0 || len(l.CGoBuild) > 0) {
				l.BuildWith = "go"
			}
			if l.BuildWith != "" {
				addDependency(build, l.BuildWith)
			}
		}
	}

	// build images until there are no pending builds left
	done := make(chan *Build, len(builds))
	failures := make(map[string]error)
	for len(builds) > 0 {
		for _, build := range builds {
			// if the build has no more pending dependencies, build it
			if len(build.Dependencies) == 0 {
				build.Once.Do(func() {
					// if the build is aborted due to a dependency
					// failure, just send it to the done channel
					if build.Abort {
						b.log.Debug(fmt.Sprintf("%s build abort", build.Image.ID))
						done <- build
						return
					}

					b.log.Debug(fmt.Sprintf("%s build start", build.Image.ID))
					go func(build *Build) {
						build.StartedAt = time.Now()
						build.Err = b.BuildImage(build.Image)
						done <- build
					}(build)
				})
			}
		}

		// wait for a build to finish
		build := <-done
		b.bar.Increment()
		if build.Err == nil {
			b.log.Debug(fmt.Sprintf("%s build done", build.Image.ID), "duration", time.Since(build.StartedAt))
		} else {
			b.log.Error(fmt.Sprintf("%s build error", build.Image.ID), "duration", time.Since(build.StartedAt), "err", build.Err)
		}

		// remove from the pending list
		delete(builds, build.Image.ID)

		// remove the build as a pending dependency from all
		// dependents
		for dependent := range build.Dependents {
			// if the build failed or was aborted, abort the
			// dependent builds
			if build.Err != nil || build.Abort {
				dependent.Abort = true
			}
			dependent.RemoveDependency(build)
		}

		if build.Err != nil {
			failures[build.Image.ID] = build.Err
		}
	}

	if len(failures) > 0 {
		b.log.Error("the following builds failed:")
		for id, err := range failures {
			b.log.Error("* "+id, "err", err)
		}
		return fmt.Errorf("%d builds failed", len(failures))
	}

	return nil
}

// BuildImage builds the image's layers and adds the resulting artifact to
// b.artifacts
func (b *Builder) BuildImage(image *Image) error {
	var layers []*ct.ImageLayer
	for _, l := range image.Layers {
		name := l.Name
		if name == "" {
			name = image.ID
		}

		env := make(map[string]string, len(image.Env)+len(l.Env))
		for k, v := range image.Env {
			env[k] = v
		}
		for k, v := range l.Env {
			env[k] = v
		}

		run := make([]string, len(l.Run))
		for i, cmd := range l.Run {
			run[i] = cmd
		}

		var inputs []string

		// add the script as an input and run with 'bash -e'
		if l.Script != "" {
			inputs = append(inputs, l.Script)
			run = append(run, "bash -e "+l.Script)
		}

		// add the explicit inputs, expanding globs
		for _, input := range l.Inputs {
			paths, err := filepath.Glob(input)
			if err != nil {
				return err
			}
			inputs = append(inputs, paths...)
		}

		// if building Go binaries, load Go inputs for the configured
		// GOOS / GOARCH and build with 'go build' / 'cgo build'
		if len(l.GoBuild) > 0 || len(l.CGoBuild) > 0 {
			goInputs := b.GoInputsFor(GoPlatform{OS: env["GOOS"], Arch: env["GOARCH"]})

			// add the commands in a predictable order so the
			// generated layer ID is deterministic
			dirs := make([]string, 0, len(l.GoBuild))
			for dir := range l.GoBuild {
				dirs = append(dirs, dir)
			}
			sort.Strings(dirs)
			for _, dir := range dirs {
				i, err := goInputs.Load(dir)
				if err != nil {
					return err
				}
				inputs = append(inputs, i...)
				run = append(run, fmt.Sprintf("go build -o %s %s", l.GoBuild[dir], filepath.Join("github.com/flynn/flynn", dir)))
			}
			dirs = make([]string, 0, len(l.CGoBuild))
			for dir := range l.CGoBuild {
				dirs = append(dirs, dir)
			}
			sort.Strings(dirs)
			for _, dir := range dirs {
				i, err := goInputs.Load(dir)
				if err != nil {
					return err
				}
				inputs = append(inputs, i...)
				run = append(run, fmt.Sprintf("cgo build -o %s %s", l.CGoBuild[dir], filepath.Join("github.com/flynn/flynn", dir)))
			}
		}

		// copy the l.Copy inputs in a predictable order so the
		// generated layer ID is deterministic
		copyPaths := make([]string, 0, len(l.Copy))
		for path := range l.Copy {
			copyPaths = append(copyPaths, path)
		}
		sort.Strings(copyPaths)
		for _, path := range copyPaths {
			inputs = append(inputs, path)
			dst := l.Copy[path]
			run = append(run, fmt.Sprintf("mkdir -p %q && cp %q %q", filepath.Dir(dst), path, dst))
		}

		// run the build job with either l.BuildWith or image.Base
		var artifact *ct.Artifact
		var err error
		if l.BuildWith != "" {
			artifact, err = b.Artifact(l.BuildWith)
		} else if image.Base != "" {
			artifact, err = b.Artifact(image.Base)
		}
		if err != nil {
			return err
		}

		// interpolate the environment variables
		for k, v := range env {
			tmpl, err := template.New("env").Parse(v)
			if err != nil {
				return fmt.Errorf("error parsing env template %q: %s", v, err)
			}
			var buf bytes.Buffer
			if err := tmpl.Execute(&buf, b.envTemplateData); err != nil {
				return fmt.Errorf("error parsing env template %q: %s", v, err)
			}
			env[k] = buf.String()
		}

		// generate the layer ID from the layer config, artifact and
		// list of inputs
		id, err := b.generateLayerID(name, run, env, artifact, inputs...)
		if err != nil {
			return err
		}

		start := time.Now()
		layer, err := b.BuildLayer(l, id, name, run, env, artifact, inputs)
		if err != nil {
			return err
		}
		b.log.Debug(fmt.Sprintf("%s layer done", name), "layer.id", id, "duration", time.Since(start))
		layers = append(layers, layer)
	}

	// generate an artifact based on image.Base and add to b.artifacts
	var baseLayers []*ct.ImageLayer
	if image.Base != "" {
		baseArtifact, err := b.Artifact(image.Base)
		if err != nil {
			return err
		}
		for _, rootfs := range baseArtifact.Manifest().Rootfs {
			baseLayers = append(baseLayers, rootfs.Layers...)
		}
	}
	manifest := ct.ImageManifest{
		Type: ct.ImageManifestTypeV1,
		Rootfs: []*ct.ImageRootfs{{
			Platform: ct.DefaultImagePlatform,
			Layers:   append(baseLayers, layers...),
		}},
	}
	if image.Entrypoint != nil {
		manifest.Entrypoints = map[string]*ct.ImageEntrypoint{
			"_default": image.Entrypoint,
		}
	}
	imageURL := fmt.Sprintf("%s?name=%s&target=/images/%s.json", b.tufConfig.Repository, image.ID, manifest.ID())
	artifact := &ct.Artifact{
		Type:             ct.ArtifactTypeFlynn,
		URI:              imageURL,
		RawManifest:      manifest.RawManifest(),
		Hashes:           manifest.Hashes(),
		Size:             int64(len(manifest.RawManifest())),
		LayerURLTemplate: layerURLTemplate,
		Meta: map[string]string{
			"manifest.id":        manifest.ID(),
			"flynn.component":    image.ID,
			"flynn.system-image": "true",
		},
	}
	b.artifactsMtx.Lock()
	b.artifacts[image.ID] = artifact
	b.artifactsMtx.Unlock()

	// write the artifact to build/image/ID.json
	path := filepath.Join("build", "image", image.ID+".json")
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return json.NewEncoder(f).Encode(artifact)
}

var imageArtifactPattern = regexp.MustCompile(`\$image_artifact\[[^\]]+\]`)

// WriteManifests interpolates a set of manifests and writes them to the
// build/manifests directory
func (b *Builder) WriteManifests(manifests map[string]string, tufRepository string) error {
	for src, name := range manifests {
		dst := filepath.Join("build", "manifests", name)
		b.log.Debug("writing manifest", "src", src, "dst", dst)

		manifest, err := ioutil.ReadFile(src)
		if err != nil {
			return err
		}
		var replaceErr error
		manifest = imageArtifactPattern.ReplaceAllFunc(manifest, func(raw []byte) []byte {
			name := string(raw[16 : len(raw)-1])
			artifact, ok := b.artifacts[name]
			if !ok {
				replaceErr = fmt.Errorf("unknown image %q", name)
				return nil
			}
			artifact.Meta = map[string]string{
				"flynn.component":    name,
				"flynn.system-image": "true",
			}
			data, err := json.Marshal(artifact)
			if err != nil {
				replaceErr = err
				return nil
			}
			return data
		})
		if replaceErr != nil {
			return replaceErr
		}
		if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
			return err
		}
		if err := ioutil.WriteFile(dst, manifest, 0644); err != nil {
			return err
		}
	}
	return nil
}

// WriteImages writes the built images to build/images.json
func (b *Builder) WriteImages() error {
	path := "build/images.json"
	tmp, err := os.Create(path + ".tmp")
	if err != nil {
		return err
	}
	defer tmp.Close()
	if err := json.NewEncoder(tmp).Encode(b.artifacts); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	return os.Rename(tmp.Name(), path)
}

func (b *Builder) Artifact(name string) (*ct.Artifact, error) {
	b.artifactsMtx.RLock()
	defer b.artifactsMtx.RUnlock()
	artifact, ok := b.artifacts[name]
	if !ok {
		return nil, fmt.Errorf("missing %q artifact", name)
	}
	return artifact, nil
}

// GetCachedLayer gets a layer either from the local /var/lib/flynn/layer-cache
// directory or from the TUF repository, returning a nil layer for a cache miss
func (b *Builder) GetCachedLayer(name, id string) (*ct.ImageLayer, error) {
	// first check the local cache
	f, err := os.Open(b.layerConfigPath(id))
	if err == nil {
		defer f.Close()
		layer := &ct.ImageLayer{}
		return layer, json.NewDecoder(f).Decode(layer)
	} else if !os.IsNotExist(err) {
		return nil, err
	}

	// not found locally, check the TUF repo
	data, err := tufutil.DownloadString(b.tufClient, fmt.Sprintf("/layers/%s.json", id))
	if _, ok := err.(tuf.ErrUnknownTarget); ok {
		// cache miss, return a nil layer so it gets generated
		return nil, nil
	} else if err != nil {
		return nil, fmt.Errorf("error getting layer from the TUF repo: %s", err)
	}
	layer := &ct.ImageLayer{}
	if err := json.Unmarshal([]byte(data), layer); err != nil {
		return nil, fmt.Errorf("error getting layer from the TUF repo: %s", err)
	}

	// cache the layer locally
	b.log.Info("fetching layer", "layer.name", name, "layer.id", id, "layer.size", units.BytesSize(float64(layer.Length)))
	tmp, err := tufutil.Download(b.tufClient, fmt.Sprintf("/layers/%s.squashfs", id))
	if err != nil {
		return nil, fmt.Errorf("error getting layer from the TUF repo: %s", err)
	}
	defer tmp.Close()
	f, err = os.Create(b.layerPath(id))
	if err != nil {
		return nil, err
	}
	defer f.Close()
	if _, err := io.Copy(f, tmp); err != nil {
		return nil, fmt.Errorf("error writing layer to cache: %s", err)
	}
	if err := ioutil.WriteFile(b.layerConfigPath(id), []byte(data), 0644); err != nil {
		return nil, fmt.Errorf("error writing layer to cache: %s", err)
	}
	return layer, nil
}

// BuildLayer either returns a cached layer or runs a job to build the layer
func (b *Builder) BuildLayer(l *Layer, id, name string, run []string, env map[string]string, artifact *ct.Artifact, inputs []string) (*ct.ImageLayer, error) {
	// try and get the cached layer first
	layer, err := b.GetCachedLayer(name, id)
	if err != nil {
		return nil, err
	} else if layer != nil {
		return layer, nil
	}

	// create a shared directory containing the inputs so we can ensure the
	// job only accesses declared inputs (thus enforcing the correctness of
	// the generated layer ID)
	dir, err := ioutil.TempDir("", "flynn-build-mnt")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)
	if err := os.Chmod(dir, 0755); err != nil {
		return nil, err
	}
	for _, subdir := range []string{"bin", "out", "src"} {
		if err := os.MkdirAll(filepath.Join(dir, subdir), 0755); err != nil {
			return nil, err
		}
	}
	copyFile := func(srcPath, dstPath string) error {
		src, err := os.Open(srcPath)
		if err != nil {
			return err
		}
		defer src.Close()
		stat, err := src.Stat()
		if err != nil {
			return err
		}
		path := filepath.Join(dir, dstPath)
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			return err
		}
		dst, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, stat.Mode())
		if err != nil {
			return err
		}
		defer dst.Close()
		_, err = io.Copy(dst, src)
		return err
	}
	for _, input := range inputs {
		if err := copyFile(input, filepath.Join("src", input)); err != nil {
			b.log.Error("error copying input", "input", input, "err", err)
			return nil, err
		}
	}

	// copy the flynn-builder binary into the shared directory so we can
	// run it inside the job
	if err := copyFile(os.Args[0], "bin/flynn-builder"); err != nil {
		b.log.Error("error copying flynn-builder binary", "err", err)
		return nil, err
	}

	job := &host.Job{
		Config: host.ContainerConfig{
			Env:        env,
			DisableLog: true,
		},
		Resources: resource.Defaults(),
		Metadata: map[string]string{
			"flynn-controller.app_name": "builder",
			"flynn-controller.type":     name,
		},
	}
	cmd := exec.Cmd{Job: job}

	// run bash inside the job, passing the commands via stdin
	job.Config.Args = []string{"/mnt/bin/flynn-builder", "run", "bash", "-exs"}
	job.Config.Stdin = true
	cmd.Stdin = strings.NewReader(strings.Join(run, "\n"))

	// set FLYNN_VERSION which will be assigned to the pkg/version.version
	// constant using ldflags when building Go binaries.
	//
	// This is not treated as an input because we only want to build a new
	// binary with the given version if the build inputs have changed.
	job.Config.Env["FLYNN_VERSION"] = b.version

	// run the job in the host network to avoid a kernel bug which causes
	// subsequent jobs to block waiting on the lo network device to become
	// free (see https://github.com/docker/docker/issues/5618).
	//
	// NOTE: this leads to an impure build, jobs sometimes use the state of
	//   the network to change the installation procedure (e.g. PostgreSQL
	//   changes the default port to 5433 if something is already listening
	//   on port 5432 at install time)
	job.Config.HostNetwork = true

	linuxCapabilities := append(host.DefaultCapabilities, l.LinuxCapabilities...)
	job.Config.LinuxCapabilities = &linuxCapabilities

	for typ, v := range l.Limits {
		limit, err := resource.ParseLimit(resource.Type(typ), v)
		if err != nil {
			return nil, fmt.Errorf("error parsing limit %q = %q: %s", typ, v, err)
		}
		job.Resources.SetLimit(resource.Type(typ), limit)
	}

	// mount the shared directory at /mnt as a 9p filesystem
	ln, err := net.Listen("tcp", os.Getenv("EXTERNAL_IP")+":0")
	if err != nil {
		return nil, err
	}
	defer ln.Close()
	fs := &go9p.Ufs{Root: dir}
	fs.Dotu = true
	fs.Start(fs)
	go fs.StartListener(ln)
	addr := ln.Addr().(*net.TCPAddr)
	job.Config.Mounts = append(job.Config.Mounts, host.Mount{
		Device:   "9p",
		Location: "/mnt",
		Target:   addr.IP.String(),
		Data:     fmt.Sprintf("trans=tcp,port=%d", addr.Port),
	})
	job.Config.WorkingDir = "/mnt/src"

	if artifact == nil {
		// use the base layer if there is no artifact to build with
		job.Mountspecs = []*host.Mountspec{b.baseLayer}
	} else {
		utils.SetupMountspecs(job, []*ct.Artifact{artifact})
	}

	// copy output to log file + prefix stdout / stderr with the layer name
	logPath := filepath.Join("build/log", name+".log")
	if err := os.MkdirAll(filepath.Dir(logPath), 0755); err != nil {
		return nil, err
	}
	logFile, err := os.Create(logPath)
	if err != nil {
		return nil, err
	}
	logR, logW := io.Pipe()
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	go io.Copy(logW, stdout)
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, err
	}
	go io.Copy(logW, stderr)
	go func() {
		defer logFile.Close()
		s := bufio.NewScanner(logR)
		for s.Scan() {
			fmt.Fprintf(os.Stderr, "%s: %s: %s\n", time.Now().Format("15:04:05.999"), name, s.Text())
			fmt.Fprintln(logFile, s.Text())
		}
	}()

	// run the job
	b.log.Info("building layer", "layer.name", name, "layer.id", id)
	if err := cmd.Run(); err != nil {
		b.log.Error("error running the build job", "name", name, "err", err)
		return nil, err
	}

	// copy the layer to the cache
	f, err := os.Open(filepath.Join(dir, "out", "layer.squashfs"))
	if err != nil {
		return nil, fmt.Errorf("error opening SquashFS layer: %s", err)
	}
	defer f.Close()
	stat, err := f.Stat()
	if err != nil {
		return nil, fmt.Errorf("error opening SquashFS layer: %s", err)
	}
	h := sha512.New512_256()
	dst, err := os.Create(b.layerPath(id))
	if err != nil {
		return nil, fmt.Errorf("error writing to layer cache: %s", err)
	}
	defer dst.Close()
	if _, err := io.Copy(dst, io.TeeReader(f, h)); err != nil {
		return nil, fmt.Errorf("error writing to layer cache: %s", err)
	}
	layer = &ct.ImageLayer{
		ID:     id,
		Type:   ct.ImageLayerTypeSquashfs,
		Length: stat.Size(),
		Hashes: map[string]string{
			"sha512_256": hex.EncodeToString(h.Sum(nil)),
		},
	}
	data, err := json.Marshal(layer)
	if err != nil {
		return nil, fmt.Errorf("error encoding layer config: %s", err)
	}
	if err := ioutil.WriteFile(b.layerConfigPath(id), data, 0644); err != nil {
		return nil, fmt.Errorf("error writing to layer cache: %s", err)
	}
	return layer, nil
}

func (b *Builder) GoInputsFor(platform GoPlatform) *GoInputs {
	b.goInputsMtx.Lock()
	defer b.goInputsMtx.Unlock()
	if g, ok := b.goInputs[platform]; ok {
		return g
	}
	g := NewGoInputs(platform)
	b.goInputs[platform] = g
	return g
}

const layerURLTemplate = "file:///var/lib/flynn/layer-cache/{id}.squashfs"

func (b *Builder) layerPath(id string) string {
	return fmt.Sprintf("/var/lib/flynn/layer-cache/%s.squashfs", id)
}

func (b *Builder) layerConfigPath(id string) string {
	return fmt.Sprintf("/var/lib/flynn/layer-cache/%s.json", id)
}

// generateLayerID generates a layer ID from a set of all inputs required to
// build the layer, which prevents rebuilding a layer if the inputs haven't
// changed.
//
// It does this by constructing a canonicalised JSON object representing the
// inputs and computing the SHA512/256 sum of the resulting bytes.
//
// TODO: consider storing a map of filenames to hashes and cache based
//       on the last modified time to avoid unnecessary work.
func (b *Builder) generateLayerID(name string, run []string, env map[string]string, artifact *ct.Artifact, inputs ...string) (id string, err error) {
	start := time.Now()
	defer func() {
		b.log.Debug("generated layer ID", "name", name, "id", id, "duration", time.Since(start))
	}()

	type fileInput struct {
		Path string `json:"path"`
		Size int64  `json:"size"`
		SHA  string `json:"sha"`
	}
	var layer = struct {
		Name        string            `json:"name"`
		Run         []string          `json:"run,omitempty"`
		Env         map[string]string `json:"env,omitempty"`
		RawManifest json.RawMessage   `json:"manifest,omitempty"`
		Files       []*fileInput      `json:"files,omitempty"`
	}{
		Name:  name,
		Run:   run,
		Env:   env,
		Files: make([]*fileInput, 0, len(inputs)),
	}
	if artifact != nil {
		layer.RawManifest = artifact.RawManifest
	}
	addFile := func(path string) error {
		f, err := os.Open(path)
		if err != nil {
			return err
		}
		defer f.Close()
		stat, err := f.Stat()
		if err != nil {
			return err
		}
		h := sha512.New512_256()
		if _, err := io.Copy(h, f); err != nil {
			return err
		}
		layer.Files = append(layer.Files, &fileInput{
			Path: path,
			Size: stat.Size(),
			SHA:  hex.EncodeToString(h.Sum(nil)),
		})
		return nil
	}
	for _, input := range inputs {
		if err := addFile(input); err != nil {
			return "", err
		}
	}
	data, err := cjson.Marshal(layer)
	if err != nil {
		return "", err
	}
	sum := sha512.Sum512_256(data)
	return hex.EncodeToString(sum[:]), nil
}

// NewProgressBar creates a progress bar which is pinned to the bottom of the
// terminal screen
func NewProgressBar(count int, tty bool) (*pb.ProgressBar, error) {
	bar := pb.New(count)

	if !tty {
		bar.Output = os.Stderr
		return bar, nil
	}

	// replace os.Stdout / os.Stderr with a pipe and copy output to a
	// channel so that the progress bar can be wiped before printing output
	type stdOutput struct {
		Out  io.Writer
		Text string
	}
	output := make(chan *stdOutput)
	wrap := func(out io.Writer) (*os.File, error) {
		r, w, err := os.Pipe()
		if err != nil {
			return nil, err
		}
		go func() {
			s := bufio.NewScanner(r)
			for s.Scan() {
				output <- &stdOutput{out, s.Text()}
			}
		}()
		return w, nil
	}
	stdout := os.Stdout
	var err error
	os.Stdout, err = wrap(stdout)
	if err != nil {
		return nil, err
	}
	stderr := os.Stderr
	os.Stderr, err = wrap(stderr)
	if err != nil {
		return nil, err
	}

	progress := make(chan string)
	bar.Callback = func(out string) { progress <- out }
	go func() {
		var barText string
		for {
			select {
			case out := <-output:
				// if we have printed the bar, replace it with
				// spaces then write the output on the same line
				if len(barText) > 0 {
					spaces := make([]byte, len(barText))
					for i := 0; i < len(barText); i++ {
						spaces[i] = ' '
					}
					fmt.Fprint(stderr, "\r", string(spaces), "\r")
				}
				fmt.Fprintln(out.Out, out.Text)

				// re-print the bar on the next line
				if len(barText) > 0 {
					fmt.Fprint(stderr, "\r"+barText)
				}
			case out := <-progress:
				// print the bar over the previous bar
				barText = out
				fmt.Fprint(stderr, "\r"+out)
			}
		}
	}()

	return bar, nil
}

type GoPlatform struct {
	OS   string
	Arch string
}

// GoInputs determines all the Go files which are required to build a Go
// program contained in a directory.
//
// It skips stdlib files as they are contained within the Go image so do not
// need to be treated as file inputs.
type GoInputs struct {
	ctx    build.Context
	srcDir string
	inputs map[string][]string
	mtx    sync.RWMutex
	loader singleflight.Group
}

func NewGoInputs(platform GoPlatform) *GoInputs {
	srcDir, _ := os.Getwd()
	ctx := build.Default
	ctx.CgoEnabled = true
	if platform.OS != "" {
		ctx.GOOS = platform.OS
	}
	if platform.Arch != "" {
		ctx.GOARCH = platform.Arch
	}
	return &GoInputs{
		ctx:    ctx,
		srcDir: srcDir,
		inputs: make(map[string][]string),
	}
}

func (g *GoInputs) Load(dir string) ([]string, error) {
	p, err := g.ctx.ImportDir(dir, 0)
	if err != nil {
		return nil, err
	}
	inputs := make([]string, len(p.GoFiles))
	for i, file := range p.GoFiles {
		inputs[i] = filepath.Join(dir, file)
	}
	for _, pkg := range p.Imports {
		imports, err := g.load(pkg)
		if err != nil {
			return nil, err
		}
		inputs = append(inputs, imports...)
	}
	return inputs, nil
}

func (g *GoInputs) load(pkg string) ([]string, error) {
	g.mtx.RLock()
	cached, ok := g.inputs[pkg]
	g.mtx.RUnlock()
	if ok {
		return cached, nil
	}

	inputs, err := g.loader.Do(pkg, func() (interface{}, error) {
		if pkg == "C" {
			return []string{}, nil
		}

		// load the package
		p, err := g.ctx.Import(pkg, g.srcDir, 0)
		if err != nil {
			return nil, err
		}

		// skip standard lib packages (they exist in the Go image)
		if p.Goroot {
			g.mtx.Lock()
			g.inputs[pkg] = []string{}
			g.mtx.Unlock()
			return []string{}, nil
		}

		// add the source files
		files := p.GoFiles
		files = append(files, p.CgoFiles...)
		files = append(files, p.CFiles...)
		files = append(files, p.SFiles...)
		files = append(files, p.IgnoredGoFiles...)

		inputs := make([]string, len(files))
		for i, file := range files {
			path, _ := filepath.Rel(g.srcDir, filepath.Join(p.Dir, file))
			inputs[i] = path
		}

		// recursively add imported packages
		for _, pkg := range p.Imports {
			imports, err := g.load(pkg)
			if err != nil {
				return nil, err
			}
			inputs = append(inputs, imports...)
		}

		return inputs, nil
	})
	if err != nil {
		return nil, err
	}

	g.mtx.Lock()
	g.inputs[pkg] = inputs.([]string)
	g.mtx.Unlock()
	return inputs.([]string), nil
}
