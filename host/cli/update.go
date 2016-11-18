package cli

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"os"
	"path"
	"sync"
	"syscall"

	"github.com/docker/docker/pkg/term"
	"github.com/docker/go-units"
	ct "github.com/flynn/flynn/controller/types"
	"github.com/flynn/flynn/pkg/cluster"
	"github.com/flynn/flynn/pkg/dialer"
	"github.com/flynn/flynn/pkg/exec"
	"github.com/flynn/flynn/pkg/tufutil"
	"github.com/flynn/flynn/pkg/version"
	"github.com/flynn/go-docopt"
	tuf "github.com/flynn/go-tuf/client"
	"gopkg.in/inconshreveable/log15.v2"
)

func init() {
	Register("update", runUpdate, `
usage: flynn-host update [options]

Options:
  -r --repository=<uri>    image repository URI [default: https://dl.flynn.io/tuf]
  -t --tuf-db=<path>       local TUF file [default: /etc/flynn/tuf.db]
  -b --bin-dir=<dir>       directory to download binaries to [default: /usr/local/bin]
  -c --config-dir=<dir>    directory to download config files to [default: /etc/flynn]
  --is-latest              internal flag (skip updating local tuf DB and re-execing latest binary)
  --is-tempfile            internal flag (binary is a temp file which requires removal)

Update Flynn components`)
}

// minVersion is the minimum version that can be updated from.
//
// The current minimum version is v20161108.0 since versions prior to that used
// a different image format
var minVersion = "v20161108.0"

var ErrIncompatibleVersion = fmt.Errorf(`
Versions prior to %s cannot be updated in-place to this version of Flynn.
In order to update to this version a cluster backup/restore is required.
Please see the updating documentation at https://flynn.io/docs/production#backup/restore.
`[1:], minVersion)

func runUpdate(args *docopt.Args) error {
	log := log15.New()

	// create and update a TUF client
	log.Info("initializing TUF client")
	local, err := tuf.FileLocalStore(args.String["--tuf-db"])
	if err != nil {
		log.Error("error creating local TUF client", "err", err)
		return err
	}
	remote, err := tuf.HTTPRemoteStore(args.String["--repository"], tufHTTPOpts("updater"))
	if err != nil {
		log.Error("error creating remote TUF client", "err", err)
		return err
	}
	client := tuf.NewClient(local, remote)

	if !args.Bool["--is-latest"] {
		return updateAndExecLatest(args.String["--config-dir"], client, log)
	}

	// unlink the current binary if it is a temp file
	if args.Bool["--is-tempfile"] {
		os.Remove(os.Args[0])
	}

	// read the TUF db so we can pass it to hosts
	log.Info("reading TUF database")
	tufDB, err := ioutil.ReadFile(args.String["--tuf-db"])
	if err != nil {
		log.Error("error reading the TUF database", "err", err)
		return err
	}

	log.Info("getting host list")
	clusterClient := cluster.NewClient()
	hosts, err := clusterClient.Hosts()
	if err != nil {
		log.Error("error getting host list", "err", err)
		return err
	}
	if len(hosts) == 0 {
		return errors.New("no hosts found")
	}

	log.Info(fmt.Sprintf("updating %d hosts", len(hosts)))

	// eachHost invokes the given function in a goroutine for each host,
	// returning an error if any of the functions returns an error.
	eachHost := func(f func(*cluster.Host, log15.Logger) error) (err error) {
		errs := make(chan error)
		for _, h := range hosts {
			go func(host *cluster.Host) {
				log := log.New("host", host.ID())
				errs <- f(host, log)
			}(h)
		}
		for range hosts {
			if e := <-errs; e != nil {
				err = e
			}
		}
		return
	}

	log.Info("checking host version compatibility")
	if err := eachHost(func(host *cluster.Host, log log15.Logger) error {
		status, err := host.GetStatus()
		if err != nil {
			log.Error("error getting host status", "err", err)
			return err
		}
		v := version.Parse(status.Version)
		if v.Before(version.Parse(minVersion)) && !v.Dev {
			log.Error(ErrIncompatibleVersion.Error(), "version", status.Version)
			return ErrIncompatibleVersion
		}
		return nil
	}); err != nil {
		return err
	}

	var mtx sync.Mutex
	images := make(map[string]*ct.Artifact)
	log.Info("pulling latest images on all hosts")
	if err := eachHost(func(host *cluster.Host, log log15.Logger) error {
		log.Info("pulling images")
		ch := make(chan *ct.ImagePullInfo)
		stream, err := host.PullImages(
			args.String["--repository"],
			args.String["--config-dir"],
			version.String(),
			bytes.NewReader(tufDB),
			ch,
		)
		if err != nil {
			log.Error("error pulling images", "err", err)
			return err
		}
		defer stream.Close()
		for info := range ch {
			switch info.Type {
			case ct.ImagePullTypeImage:
				log.Info(fmt.Sprintf("pulling %s image", info.Name))
				mtx.Lock()
				images[info.Name] = info.Artifact
				mtx.Unlock()
			case ct.ImagePullTypeLayer:
				log.Info(fmt.Sprintf("pulling %s layer %s (%s)",
					info.Name, info.Layer.ID, units.BytesSize(float64(info.Layer.Length))))
			}
		}
		if err := stream.Err(); err != nil {
			log.Error("error pulling images", "err", err)
			return err
		}
		return nil
	}); err != nil {
		return err
	}

	var binaries map[string]string
	log.Info("pulling latest binaries and config on all hosts")
	if err := eachHost(func(host *cluster.Host, log log15.Logger) error {
		log.Info("pulling binaries and config")
		paths, err := host.PullBinariesAndConfig(
			args.String["--repository"],
			args.String["--bin-dir"],
			args.String["--config-dir"],
			version.String(),
			bytes.NewReader(tufDB),
		)
		if err != nil {
			log.Error("error pulling binaries and config", "err", err)
			return err
		}
		mtx.Lock()
		binaries = paths
		mtx.Unlock()
		log.Info("binaries and config pulled successfully")
		return nil
	}); err != nil {
		return err
	}

	log.Info("validating binaries")
	flynnHost, ok := binaries["flynn-host"]
	if !ok {
		return fmt.Errorf("missing flynn-host binary")
	}
	flynnInit, ok := binaries["flynn-init"]
	if !ok {
		return fmt.Errorf("missing flynn-init binary")
	}

	log.Info("updating flynn-host daemon on all hosts")
	if err := eachHost(func(host *cluster.Host, log log15.Logger) error {
		// TODO(lmars): handle daemons using custom flags (e.g. --state=/foo)
		_, err := host.Update(
			flynnHost,
			"daemon",
			"--id", host.ID(),
			"--flynn-init", flynnInit,
		)
		if err != nil {
			log.Error("error updating binaries", "err", err)
			return err
		}
		log.Info("flynn-host updated successfully")
		return nil
	}); err != nil {
		return err
	}

	updaterImage, ok := images["updater"]
	if !ok {
		e := "missing updater image"
		log.Error(e)
		return errors.New(e)
	}
	imageJSON, err := json.Marshal(images)
	if err != nil {
		log.Error("error encoding images", "err", err)
		return err
	}

	// create a new cluster client to avoid reusing TCP connections to hosts
	// which are shutting down as part of the update
	clusterClient = cluster.NewClientWithHTTP(nil, &http.Client{Transport: &http.Transport{Dial: dialer.Retry.Dial}})

	// use a flag to determine whether to use a TTY log formatter because actually
	// assigning a TTY to the job causes reading images via stdin to fail.
	cmd := exec.CommandUsingCluster(
		clusterClient,
		updaterImage,
		"/bin/updater", fmt.Sprintf("--tty=%t", term.IsTerminal(os.Stdout.Fd())),
	)
	cmd.Stdin = bytes.NewReader(imageJSON)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return err
	}
	log.Info("update complete")
	return nil
}

// updateAndExecLatest updates the tuf DB, downloads the latest flynn-host
// binary to a temp file and execs it.
//
// Latest snapshot errors are ignored because, even though we may have the
// latest snapshot, the cluster may not be fully up to date (a previous update
// may have failed).
func updateAndExecLatest(configDir string, client *tuf.Client, log log15.Logger) error {
	log.Info("updating TUF data")
	if _, err := client.Update(); err != nil && !tuf.IsLatestSnapshot(err) {
		log.Error("error updating TUF client", "err", err)
		return err
	}

	version, err := getChannelVersion(configDir, client, log)
	if err != nil {
		return err
	}

	log.Info(fmt.Sprintf("downloading %s flynn-host binary", version))
	gzTmp, err := tufutil.Download(client, path.Join(version, "flynn-host.gz"))
	if err != nil {
		log.Error("error downloading latest flynn-host binary", "err", err)
		return err
	}
	defer gzTmp.Close()

	gz, err := gzip.NewReader(gzTmp)
	if err != nil {
		log.Error("error creating gzip reader", "err", err)
		return err
	}
	defer gz.Close()

	tmp, err := ioutil.TempFile("", "flynn-host")
	if err != nil {
		log.Error("error creating temp file", "err", err)
		return err
	}
	_, err = io.Copy(tmp, gz)
	tmp.Close()
	if err != nil {
		log.Error("error decompressing gzipped flynn-host binary", "err", err)
		return err
	}
	if err := os.Chmod(tmp.Name(), 0755); err != nil {
		log.Error("error setting executable bit on tmp file", "err", err)
		return err
	}

	log.Info("executing latest flynn-host binary")
	argv := []string{tmp.Name()}
	argv = append(argv, os.Args[1:]...)
	argv = append(argv, "--is-latest")
	argv = append(argv, "--is-tempfile")
	return syscall.Exec(tmp.Name(), argv, os.Environ())
}
