package main

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"

	"github.com/docker/go-units"
	ct "github.com/flynn/flynn/controller/types"
	"github.com/flynn/go-docopt"
	"github.com/flynn/go-tuf"
	tufdata "github.com/flynn/go-tuf/data"
	"github.com/flynn/go-tuf/util"
	"gopkg.in/inconshreveable/log15.v2"
)

var cmdExport = Command{
	Run: runExport,
	Usage: `
usage: flynn-builder export <tuf-dir>

Export Flynn binaries, manifests & images to a TUF repository.
`[1:],
}

func runExport(args *docopt.Args) error {
	version, err := determineVersion()
	if err != nil {
		return err
	}

	manifest, err := loadManifest()
	if err != nil {
		return err
	}

	dir := args.String["<tuf-dir>"]
	tufRepo, err := newTufRepo(dir)
	if err != nil {
		return fmt.Errorf("error creating TUF store: %s", err)
	}
	targets, err := tufRepo.Targets()
	if err != nil {
		return fmt.Errorf("error getting TUF targets: %s", err)
	}

	if err := tufRepo.Clean(); err != nil {
		return fmt.Errorf("error cleaning TUF store: %s", err)
	}

	targetMeta, _ := json.Marshal(map[string]string{"version": version})
	log := log15.New()
	log.Info("exporting Flynn", "version", version, "tuf.repository", manifest.TUFConfig.Repository)
	exporter := &Exporter{
		dir:        dir,
		tuf:        tufRepo,
		targets:    targets,
		targetMeta: targetMeta,
		repository: manifest.TUFConfig.Repository,
		log:        log,
	}
	if err := exporter.Export(version); err != nil {
		log.Error("error exporting Flynn", "err", err)
		return err
	}

	log.Info("TUF snapshot")
	if err := tufRepo.Snapshot(tuf.CompressionTypeNone); err != nil {
		log.Error("TUF snapshot error", "err", err)
		return err
	}

	log.Info("TUF timestamp")
	if err := tufRepo.Timestamp(); err != nil {
		log.Error("TUF timestamp error", "err", err)
		return err
	}

	log.Info("TUF commit")
	if err := tufRepo.Commit(); err != nil {
		log.Error("TUF commit error", "err", err)
		return err
	}

	return nil

}

func determineVersion() (string, error) {
	out, err := exec.Command("build/bin/flynn-host", "version").CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("error getting flynn-host version: %s: %s", err, out)
	}
	return string(bytes.TrimSpace(out)), nil
}

type Exporter struct {
	dir        string
	tuf        *tuf.Repo
	targets    tufdata.Files
	targetMeta json.RawMessage
	repository string
	log        log15.Logger
}

func (e *Exporter) Export(version string) error {
	// add binaries and manifests to a versioned directory so that
	// flynn-host can download them using a specific version
	// e.g. build/bin/flynn-host        => v20161229.1/flynn-host.gz
	//      build/manifests/images.json => v20161229.1/images.json.gz
	for _, bin := range []string{"flynn-host", "flynn-init", "flynn-linux-amd64"} {
		target := filepath.Join(version, bin)
		if err := e.ExportBinary(bin, target); err != nil {
			return fmt.Errorf("error exporting %s: %s", bin, err)
		}
	}
	for _, manifest := range []string{"bootstrap-manifest.json", "images.json"} {
		target := filepath.Join(version, manifest)
		if err := e.ExportManifest(manifest, target); err != nil {
			return fmt.Errorf("error exporting %s: %s", manifest, err)
		}
	}

	// add the flynn-host binary at the top level so it can be found by the install script
	if err := e.ExportBinary("flynn-host", "flynn-host"); err != nil {
		return fmt.Errorf("error exporting top-level flynn-host: %s", err)
	}

	// add the CLIs at the top-level for installation and 'flynn update'
	bins := []string{
		"flynn-linux-amd64",
		"flynn-linux-386",
		"flynn-darwin-amd64",
		"flynn-freebsd-amd64",
		"flynn-windows-amd64",
		"flynn-windows-386",
	}
	for _, bin := range bins {
		if err := e.ExportBinary(bin, bin); err != nil {
			return fmt.Errorf("error exporting %s: %s", bin, err)
		}
	}

	// add images + layers
	artifacts, err := e.loadArtifacts()
	if err != nil {
		return err
	}
	for name, artifact := range artifacts {
		e.log.Info(fmt.Sprintf("exporting %s image", name), "name", name, "image.id", artifact.Manifest().ID())
		if err := e.ExportImage(name, artifact); err != nil {
			return fmt.Errorf("error exporting %s image: %s", name, err)
		}
	}
	return nil
}

func (e *Exporter) ExportBinary(name, target string) error {
	e.log.Info(fmt.Sprintf("exporting %s", name), "target", target)
	f, err := os.Open(filepath.Join("build", "bin", name))
	if err != nil {
		return err
	}
	defer f.Close()
	return e.ExportData(f, target)
}

func (e *Exporter) ExportManifest(name, target string) error {
	e.log.Info(fmt.Sprintf("exporting %s", name), "target", target)
	manifest, err := ioutil.ReadFile(filepath.Join("build", "manifests", name))
	if err != nil {
		return fmt.Errorf("error reading manifest %s: %s", name, err)
	}

	// rewrite layer URIs to point at the TUF repo
	tufLayerURLTemplate := fmt.Sprintf("%s?target=/layers/{id}.squashfs", e.repository)
	manifest = bytes.Replace(manifest, []byte(layerURLTemplate), []byte(tufLayerURLTemplate), -1)

	return e.ExportData(bytes.NewReader(manifest), target)
}

func (e *Exporter) ExportData(data io.Reader, target string) error {
	target = target + ".gz"
	path := e.stagedPath(target)
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	dst, err := os.Create(path)
	if err != nil {
		return err
	}
	defer dst.Close()
	gz, err := gzip.NewWriterLevel(dst, gzip.BestCompression)
	if err != nil {
		os.Remove(dst.Name())
		return err
	}
	if _, err := io.Copy(gz, data); err != nil {
		gz.Close()
		os.Remove(dst.Name())
		return err
	}
	if err := gz.Close(); err != nil {
		return err
	}
	return e.addTarget(target)
}

func (e *Exporter) ExportImage(name string, artifact *ct.Artifact) error {
	log := e.log.New("name", name)

	id := artifact.Manifest().ID()
	target := e.imageTarget(id)
	if _, ok := e.targets[target]; ok {
		// image already exists
		return nil
	}

	for _, rootfs := range artifact.Manifest().Rootfs {
		for _, layer := range rootfs.Layers {
			log.Info("exporting layer", "layer.id", layer.ID, "layer.size", units.BytesSize(float64(layer.Length)))
			if err := e.ExportLayer(artifact.LayerURL(layer), layer); err != nil {
				return err
			}
		}
	}

	path := e.stagedPath(target)
	log.Info("writing image manifest", "target", target)
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	if err := ioutil.WriteFile(path, artifact.RawManifest, 0644); err != nil {
		return err
	}

	return e.addTarget(target)
}

func (e *Exporter) ExportLayer(uri string, layer *ct.ImageLayer) error {
	layerTarget := e.layerTarget(layer)
	if _, ok := e.targets[layerTarget]; ok {
		// layer already exists
		return nil
	}

	if layer.Type != ct.ImageLayerTypeSquashfs {
		return fmt.Errorf("unknown layer type %q", layer.Type)
	}

	layerConfigTarget := e.layerConfigTarget(layer)
	layerConfigPath := e.stagedPath(layerConfigTarget)
	if err := os.MkdirAll(filepath.Dir(layerConfigPath), 0755); err != nil {
		return err
	}
	f, err := os.Create(layerConfigPath)
	if err != nil {
		return err
	}
	defer f.Close()
	if err := json.NewEncoder(f).Encode(layer); err != nil {
		return err
	}
	if err := e.addTarget(layerConfigTarget); err != nil {
		return err
	}

	f, err = os.Open(fmt.Sprintf("/var/lib/flynn/layer-cache/%s.squashfs", layer.ID))
	if err != nil {
		return err
	}
	defer f.Close()

	layerPath := e.stagedPath(layerTarget)
	if err := os.MkdirAll(filepath.Dir(layerPath), 0755); err != nil {
		return err
	}
	dst, err := os.Create(layerPath)
	if err != nil {
		return err
	}
	defer dst.Close()

	n, err := io.Copy(dst, f)
	if err != nil {
		return err
	} else if n != layer.Length {
		return fmt.Errorf("error copying layer: expected to write %d bytes but copied %d", layer.Length, n)
	}

	return e.addTarget(layerTarget)
}

func (e *Exporter) loadArtifacts() (map[string]*ct.Artifact, error) {
	f, err := os.Open("build/images.json")
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var artifacts map[string]*ct.Artifact
	return artifacts, json.NewDecoder(f).Decode(&artifacts)
}

func (e *Exporter) addTarget(target string) error {
	return e.tuf.AddTarget(target, e.targetMeta)
}

func (e *Exporter) imageTarget(id string) string {
	return util.NormalizeTarget(path.Join("images", id+".json"))
}

func (e *Exporter) layerTarget(layer *ct.ImageLayer) string {
	return util.NormalizeTarget(path.Join("layers", layer.ID+".squashfs"))
}

func (e *Exporter) layerConfigTarget(layer *ct.ImageLayer) string {
	return util.NormalizeTarget(path.Join("layers", layer.ID+".json"))
}

func (e *Exporter) stagedPath(target string) string {
	return filepath.Join(e.dir, "staged", "targets", target)
}

func newTufRepo(dir string) (*tuf.Repo, error) {
	stat, err := os.Stat(dir)
	if err != nil {
		return nil, err
	} else if !stat.IsDir() {
		return nil, fmt.Errorf("not a directory: %s", dir)
	}
	store := tuf.FileSystemStore(dir, tufPassphrase)
	return tuf.NewRepo(store)
}

func tufPassphrase(role string, confirm bool) ([]byte, error) {
	return []byte(os.Getenv(fmt.Sprintf("TUF_%s_PASSPHRASE", strings.ToUpper(role)))), nil
}
