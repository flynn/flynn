package main

import (
	"bytes"
	"compress/gzip"
	"errors"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"

	cfg "github.com/flynn/flynn/cli/config"
	"github.com/flynn/flynn/pkg/random"
	"github.com/flynn/flynn/pkg/tufconfig"
	"github.com/flynn/flynn/pkg/tufutil"
	"github.com/flynn/flynn/pkg/version"
	tuf "github.com/flynn/go-tuf/client"
	"github.com/kardianos/osext"
	"gopkg.in/inconshreveable/go-update.v0"
)

const upcktimePath = "cktime"

var updateDir = filepath.Join(cfg.Dir(), "update")
var updater = &Updater{}

func runUpdate() error {
	if version.Dev() {
		return errors.New("Dev builds don't support auto-updates")
	}
	return updater.update()
}

type Updater struct{}

func (u *Updater) backgroundRun() {
	if u == nil {
		return
	}
	if !u.wantUpdate() {
		return
	}
	self, err := osext.Executable()
	if err != nil {
		// fail update, couldn't figure out path to self
		return
	}
	// TODO(titanous): logger isn't on Windows. Replace with proper error reports.
	l := exec.Command("logger", "-tflynn")
	c := exec.Command(self, "update")
	if w, err := l.StdinPipe(); err == nil && l.Start() == nil {
		c.Stdout = w
		c.Stderr = w
	}
	c.Start()
}

func (u *Updater) wantUpdate() bool {
	path := filepath.Join(updateDir, upcktimePath)
	if version.Dev() || readTime(path).After(time.Now()) {
		return false
	}
	wait := 12*time.Hour + randDuration(8*time.Hour)
	return writeTime(path, time.Now().Add(wait))
}

func (u *Updater) update() error {
	up := update.New()
	if err := up.CanUpdate(); err != nil {
		return err
	}

	if err := os.MkdirAll(updateDir, 0755); err != nil {
		return err
	}
	local, err := tuf.FileLocalStore(filepath.Join(updateDir, "tuf.db"))
	if err != nil {
		return err
	}
	plat := fmt.Sprintf("%s-%s", runtime.GOOS, runtime.GOARCH)
	opts := &tuf.HTTPRemoteOptions{
		UserAgent: fmt.Sprintf("flynn-cli/%s %s", version.String(), plat),
		Retries:   tufutil.DefaultHTTPRetries,
	}
	remote, err := tuf.HTTPRemoteStore(tufconfig.Repository, opts)
	if err != nil {
		return err
	}
	client := tuf.NewClient(local, remote)
	if err := u.updateTUFClient(client); err != nil {
		return err
	}

	name := fmt.Sprintf("/flynn-%s.gz", plat)

	latestVersion, err := tufutil.GetVersion(client, name)
	if err != nil {
		return err
	}
	if latestVersion == version.String() {
		return nil
	}

	bin := &tufBuffer{}
	if err := client.Download(name, bin); err != nil {
		return err
	}
	gr, err := gzip.NewReader(bin)
	if err != nil {
		return err
	}

	err, errRecover := up.FromStream(gr)
	if errRecover != nil {
		return fmt.Errorf("update and recovery errors: %q %q", err, errRecover)
	}
	if err != nil {
		return err
	}
	log.Printf("Updated %s -> %s.", version.String(), latestVersion)
	return nil
}

// updateTUFClient updates the given client, initializing and re-running the
// update if ErrNoRootKeys is returned.
func (u *Updater) updateTUFClient(client *tuf.Client) error {
	_, err := client.Update()
	if err == nil || tuf.IsLatestSnapshot(err) {
		return nil
	}
	if err == tuf.ErrNoRootKeys {
		if err := client.Init(tufconfig.RootKeys, len(tufconfig.RootKeys)); err != nil {
			return err
		}
		return u.updateTUFClient(client)
	}
	return err
}

// returns a random duration in [0,n).
func randDuration(n time.Duration) time.Duration {
	return time.Duration(random.Math.Int63n(int64(n)))
}

func readTime(path string) time.Time {
	p, err := ioutil.ReadFile(path)
	if os.IsNotExist(err) {
		return time.Time{}
	}
	if err != nil {
		return time.Now().Add(1000 * time.Hour)
	}
	t, err := time.Parse(time.RFC3339, string(p))
	if err != nil {
		return time.Now().Add(1000 * time.Hour)
	}
	return t
}

func writeTime(path string, t time.Time) bool {
	return ioutil.WriteFile(path, []byte(t.Format(time.RFC3339)), 0644) == nil
}

type tufBuffer struct {
	bytes.Buffer
}

func (b *tufBuffer) Delete() error {
	b.Reset()
	return nil
}
