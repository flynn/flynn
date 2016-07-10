package downloader

import (
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"

	"github.com/flynn/flynn/pkg/tufutil"
	tuf "github.com/flynn/go-tuf/client"
)

var binaries = []string{
	"flynn-host",
	"flynn-linux-amd64",
	"flynn-init",
}

var config = []string{
	"upstart.conf",
	"bootstrap-manifest.json",
}

// Downloader downloads versioned files using a tuf client
type Downloader struct {
	client  *tuf.Client
	version string
}

func New(client *tuf.Client, version string) *Downloader {
	return &Downloader{client, version}
}

// DownloadBinaries downloads the Flynn binaries using the tuf client to the
// given dir with the version suffixed (e.g. /usr/local/bin/flynn-host.v20150726.0)
// and updates non-versioned symlinks.
func (d *Downloader) DownloadBinaries(dir string) (map[string]string, error) {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("error creating bin dir: %s", err)
	}
	paths := make(map[string]string, len(binaries))
	for _, bin := range binaries {
		path, err := d.downloadGzippedFile(bin, dir, true)
		if err != nil {
			return nil, err
		}
		if err := os.Chmod(path, 0755); err != nil {
			return nil, err
		}
		paths[bin] = path
	}
	// symlink flynn to flynn-linux-amd64
	if err := symlink("flynn-linux-amd64", filepath.Join(dir, "flynn")); err != nil {
		return nil, err
	}
	return paths, nil
}

// DownloadConfig downloads the Flynn config files using the tuf client to the
// given dir.
func (d *Downloader) DownloadConfig(dir string) (map[string]string, error) {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("error creating config dir: %s", err)
	}
	paths := make(map[string]string, len(config))
	for _, conf := range config {
		path, err := d.downloadGzippedFile(conf, dir, false)
		if err != nil {
			return nil, err
		}
		paths[conf] = path
	}
	return paths, nil
}

func (d *Downloader) downloadGzippedFile(name, dir string, versionSuffix bool) (string, error) {
	path := path.Join(d.version, name)
	gzPath := path + ".gz"
	dst := filepath.Join(dir, name)
	if versionSuffix {
		dst = dst + "." + d.version
	}

	file, err := tufutil.Download(d.client, gzPath)
	if err != nil {
		return "", err
	}
	defer file.Close()

	// unlink the destination file in case it's in use
	os.Remove(dst)

	out, err := os.Create(dst)
	if err != nil {
		return "", err
	}
	defer out.Close()
	gz, err := gzip.NewReader(file)
	if err != nil {
		return "", err
	}
	defer gz.Close()
	_, err = io.Copy(out, gz)
	if err != nil {
		return "", err
	}

	if versionSuffix {
		// symlink the non-versioned path to the versioned path
		// e.g. flynn-host -> flynn-host.v20150726.0
		link := filepath.Join(dir, name)
		if err := symlink(filepath.Base(dst), link); err != nil {
			return "", err
		}
	}

	return dst, nil
}

func symlink(oldname, newname string) error {
	os.Remove(newname)
	return os.Symlink(oldname, newname)
}
