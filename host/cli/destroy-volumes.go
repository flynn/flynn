package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/flynn/go-docopt"
	"github.com/flynn/flynn/host/volume"
	"github.com/flynn/flynn/host/volume/manager"
	"github.com/flynn/flynn/host/volume/zfs"
	"github.com/flynn/flynn/pkg/shutdown"
)

func init() {
	Register("destroy-volumes", runVolumeDestroy, `
usage: flynn-host destroy-volumes [options]

options:
  --volpath=PATH     directory to create volumes in [default: /var/lib/flynn/volumes]
  --include-data     actually destroy data in backends *this is dangerous* [default: false]

Destroys all data volumes on this host.  This is a dangerous operation: data
may be permanently discarded.

If the '--include-data' flag is given, the volume database will be loaded, and
all data in the referenced systems will be removed (i.e., with a zfs backend,
the datasets will be destroyed).

If '--include-data' is not specified (or the volume database cannot be loaded),
it will simply be removed.  In this case, data will remain behind in the backend
storage engines (and eventually require manual cleanup), but the flynn-host
daemon's next launch will still be like a fresh launch.

Major features of backends will not be adjusted (i.e., zfs datasets will be
destroyed, but zpools will not be touched).`)
}

func runVolumeDestroy(args *docopt.Args) error {
	if os.Getuid() != 0 {
		fmt.Println("this command requires root!\ntry again with `sudo flynn-host destroy-volumes`.")
		shutdown.ExitWithCode(1)
	}

	volPath := args.String["--volpath"]
	includeData := args.Bool["--include-data"]

	volumeDBPath := filepath.Join(volPath, "volumes.bolt")

	// if there is no state db, nothing to do
	if _, err := os.Stat(volumeDBPath); err != nil && os.IsNotExist(err) {
		fmt.Printf("no volume state db exists at %q; already clean.\n", volumeDBPath)
		shutdown.Exit()
	}

	// open state db.  we're maybe using it; and regardless want to flock before removing it
	vman, vmanErr := loadVolumeState(volumeDBPath)

	// if '--include-data' specified and vman loaded, destroy volumes
	allVolumesDestroyed := true
	if vmanErr != nil {
		fmt.Printf("%s\n", vmanErr)
	} else if includeData == false {
		fmt.Println("'--include-data' not specified; leaving backend data storage intact.")
	} else {
		if err := destroyVolumes(vman); err != nil {
			fmt.Printf("%s\n", err)
			allVolumesDestroyed = false
		}
	}

	// remove db file
	if err := os.Remove(volumeDBPath); err != nil {
		fmt.Printf("could not remove volume state db file %q: %s.\n", volumeDBPath, err)
		shutdown.ExitWithCode(5)
	}
	fmt.Printf("state db file %q removed.\n", volumeDBPath)

	// exit code depends on if all volumes were destroyed successfully or not
	if includeData && !allVolumesDestroyed {
		shutdown.ExitWithCode(6)
	}
	shutdown.Exit()
	return nil
}

func loadVolumeState(volumeDBPath string) (*volumemanager.Manager, error) {
	// attempt to restore manager from state db
	// no need to defer closing the db; we're about to unlink it and the fd can drop on exit
	fmt.Println("opening volume state db...")
	vman := volumemanager.New(
		volumeDBPath,
		func() (volume.Provider, error) {
			return nil, nil
		},
	)
	if err := vman.OpenDB(); err != nil {
		if strings.HasSuffix(err.Error(), "timeout") { //bolt.ErrTimeout
			fmt.Println("volume state db is locked by another process; aborting.")
			shutdown.ExitWithCode(4)
		}
		return nil, fmt.Errorf("warning: the previous volume database could not be loaded; any data in backends may need manual removal\n  (error was: %s)", err)
	}
	fmt.Println("volume state db opened.")
	return vman, nil
}

func destroyVolumes(vman *volumemanager.Manager) error {
	someVolumesNotDestroyed := false
	var secondPass []string
	for volID := range vman.Volumes() {
		fmt.Printf("removing volume id=%q... ", volID)
		if err := vman.DestroyVolume(volID); err == nil {
			fmt.Println("success")
		} else if zfs.IsDatasetHasChildrenError(err) {
			fmt.Println("has children, coming back to it later")
			secondPass = append(secondPass, volID)
		} else {
			fmt.Printf("error: %s\n", err)
			someVolumesNotDestroyed = true
		}
	}
	for volID := range vman.Volumes() {
		fmt.Printf("removing volume id=%q... ", volID)
		if err := vman.DestroyVolume(volID); err == nil {
			fmt.Println("success")
		} else {
			fmt.Printf("error: %s\n", err)
			someVolumesNotDestroyed = true
		}
	}

	if someVolumesNotDestroyed {
		return fmt.Errorf("some volumes were not destroyed successfully")
	}
	return nil
}
