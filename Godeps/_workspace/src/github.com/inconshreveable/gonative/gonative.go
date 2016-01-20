package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/codegangsta/cli"
	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/inconshreveable/axiom"
	log "github.com/flynn/flynn/Godeps/_workspace/src/gopkg.in/inconshreveable/log15.v2"
)

var Log = log.Root()

const usage = `build Go installations with native stdlib packages

DESCRIPTION:
   Cross compiled Go binaries are not suitable for production applications
   because code in the standard library relies on Cgo for DNS resolution
   with the native resolver, access to system certificate roots, and parts of os/user.

   gonative is a simple tool which creates a build of Go that can cross compile
   to all platforms while still using the Cgo-enabled versions of the stdlib
   packages. It does this by downloading the binary distributions for each
   platform and copying their libraries into the proper places. It sets
   the correct access time so they don't get rebuilt. It also copies
   some auto-generated runtime files into the build as well. gonative does
   not modify any Go that you have installed and builds Go again in a separate
   directory (the current directory by default).`
const equinoxAppId = "ap_VQ_K1O_27-tPsncKE3E2GszIPm"
const updatePublicKey = `-----BEGIN PUBLIC KEY-----
MIIBIjANBgkqhkiG9w0BAQEFAAOCAQ8AMIIBCgKCAQEAvMwGMSLLi3bfq6UZesVR
H+/EnPyVqbVTJs3zCiFSnLrXMkOMuXfmf7mC23q1cPaGOIFTfmhcx5/vkda10NJ1
owTAJKXVctC6TUei42vIiBSPsdhzyinNtCdkEkBT2f6Ac58OQV1dUBW/b0fQRQZN
9tEwW7PK1QnR++bmVu2XzoGEw17XZdeDoXftDBgYAzOWDqapZpHETPobL5oQHeQN
CVdCaNbNo52/HL6XKyDGCNudVqiKgIoExPzcOL6KKfvMla1Y4mrrArbuNBlE3qxW
CwmnjtWg+J7vb9rKfZvuVPXPD/RoruZUmHBc1f31KB/QFvn/zXSqeyBcsd6ywCfo
KwIDAQAB
-----END PUBLIC KEY-----`

type Options struct {
	Version    string
	SrcPath    string
	TargetPath string
	Platforms  []Platform
}

func main() {
	app := cli.NewApp()
	app.Name = "gonative"
	app.Author = "inconshreveable"
	app.Email = "alan@inconshreveable.com"
	app.Usage = usage
	app.HideHelp = true
	app.HideVersion = true
	app.Version = "0.2.0"
	app.Commands = []cli.Command{
		cli.Command{
			Name:  "build",
			Usage: "build a go installation with native stdlib packages",
			Flags: []cli.Flag{
				cli.StringFlag{"version", "1.5.2", "version of Go to build", "", nil},
				cli.StringFlag{"src", "", "path to go source, empty string means to fetch from internet", "", nil},
				cli.StringFlag{"target", "go", "target directory in which to build Go", "", nil},
				cli.StringFlag{"platforms", "", "space separated list of platforms to build, default is 'darwin_amd64 freebsd_amd64 linux_386 linux_amd64 windows_386 windows_amd64'", "", nil},
			},
			Action: buildCmd,
		},
	}
	axiom.WrapApp(app, axiom.NewLogged())
	app.Commands = append(app.Commands, []cli.Command{
		axiom.VersionCommand(),
		axiom.NewUpdater(equinoxAppId, updatePublicKey).Command(),
	}...)
	app.Run(os.Args)
}

func buildCmd(c *cli.Context) {
	exit := func(err error) {
		if err == nil {
			os.Exit(0)
		} else {
			log.Crit("command failed", "err", err)
			os.Exit(1)
		}
	}

	opts := &Options{
		Version:    c.String("version"),
		SrcPath:    c.String("src"),
		TargetPath: c.String("target"),
	}

	platforms := c.String("platforms")
	if platforms == "" {
		opts.Platforms = defaultPlatforms
	} else {
		opts.Platforms = make([]Platform, 0)
		for _, pString := range strings.Split(platforms, " ") {
			parts := strings.Split(pString, "_")
			if len(parts) != 2 {
				exit(fmt.Errorf("Invalid platform string: %v", pString))
			}
			opts.Platforms = append(opts.Platforms, Platform{parts[0], parts[1]})
		}
	}

	exit(Build(opts))
}

func Build(opts *Options) error {
	// normalize paths
	targetPath, err := filepath.Abs(opts.TargetPath)
	if err != nil {
		return err
	}

	src := opts.SrcPath
	if src == "" {
		src = "(from internet)"
	}
	Log.Info("building go", "version", opts.Version, "src", src, "target", targetPath, "platforms", opts.Platforms)

	// tells the platform goroutines that the target path is ready
	targetReady := make(chan struct{})

	// platform gorouintes can report an error here
	errors := make(chan error, len(opts.Platforms))

	// need to wait for each platform to finish
	var wg sync.WaitGroup
	wg.Add(len(opts.Platforms))

	// run all platform fetch/copies in parallel
	for _, p := range opts.Platforms {
		go getPlatform(p, targetPath, opts.Version, targetReady, errors, &wg)
	}

	// if no source path specified, fetch source from the internet
	if opts.SrcPath == "" {
		srcPath, err := srcPlatform.Download(opts.Version)
		if err != nil {
			return err
		}
		defer os.RemoveAll(srcPath)
		opts.SrcPath = filepath.Join(srcPath, "go")
	}

	// copy the source to the target directory
	err = CopyAll(targetPath, opts.SrcPath)
	if err != nil {
		return err
	}

	// build Go for the host platform
	err = makeDotBash(targetPath)
	Log.Debug("make.bash", "err", err)
	if err != nil {
		return err
	}

	// bootstrap compilers for all target platforms
	Log.Info("boostraping go compilers")
	for _, p := range opts.Platforms {
		err = distBootstrap(targetPath, p)
		Log.Debug("bootstrap compiler", "plat", p, "err", err)
		if err != nil {
			return err
		}
	}

	// tell the platform goroutines that the target dir is ready
	close(targetReady)

	// wait for all platforms to finish
	wg.Wait()

	// return error if a platform failed
	select {
	case err := <-errors:
		return err
	default:
		Log.Info("successfuly built Go", "path", targetPath)
		return nil
	}
}

func getPlatform(p Platform, targetPath, version string, targetReady chan struct{}, errors chan error, wg *sync.WaitGroup) {
	lg := Log.New("plat", p)
	defer wg.Done()

	// download the binary distribution
	path, err := p.Download(version)
	if err != nil {
		errors <- err
		return
	}
	defer os.RemoveAll(path)

	// wait for target directory to be ready
	<-targetReady

	// copy over the packages
	targetPkgPath := filepath.Join(targetPath, "pkg", p.String())
	srcPkgPath := filepath.Join(path, "go", "pkg", p.String())
	err = CopyAll(targetPkgPath, srcPkgPath)
	if err != nil {
		errors <- err
		return
	}

	// copy over the auto-generated z_ files
	srcZPath := filepath.Join(path, "go", "src", "runtime", "z*_"+p.String())
	targetZPath := filepath.Join(targetPath, "src", "runtime")
	if version < "1.4" {
		srcZPath = filepath.Join(path, "go", "src", "pkg", "runtime", "z*_"+p.String())
		targetZPath = filepath.Join(targetPath, "src", "pkg", "runtime")
	}
	lg.Debug("copy zfile", "dst", targetZPath, "src", srcZPath, "err", err)
	CopyFile(targetZPath, srcZPath)

	// change the mod times
	now := time.Now()
	err = filepath.Walk(targetPkgPath, func(path string, info os.FileInfo, err error) error {
		os.Chtimes(path, now, now)
		return nil
	})
	lg.Debug("set modtimes", "err", err)
	if err != nil {
		errors <- err
		return
	}
}

// runs make.[bash|bat] in the source directory to build all of the compilers
// and standard library
func makeDotBash(goRoot string) (err error) {
	scriptName := "make.bash"
	if runtime.GOOS == "windows" {
		scriptName = "make.bat"
	}

	scriptPath, err := filepath.Abs(filepath.Join(goRoot, "src", scriptName))
	if err != nil {
		return
	}
	scriptDir := filepath.Dir(scriptPath)

	cmd := exec.Cmd{
		Path:   scriptPath,
		Args:   []string{scriptPath},
		Env:    os.Environ(),
		Dir:    scriptDir,
		Stdout: os.Stdout,
		Stderr: os.Stderr,
	}

	return cmd.Run()
}

// runs dist bootrap to build the compilers for a target platform
func distBootstrap(goRoot string, p Platform) (err error) {
	// the dist tool gets put in the pkg/tool/{host_platform} directory after we've built
	// the compilers/stdlib for the host platform
	hostPlatform := Platform{runtime.GOOS, runtime.GOARCH}
	scriptPath, err := filepath.Abs(filepath.Join(goRoot, "pkg", "tool", hostPlatform.String(), "dist"))
	if err != nil {
		return
	}

	// but we want to run it from the src directory
	scriptDir, err := filepath.Abs(filepath.Join(goRoot, "src"))
	if err != nil {
		return
	}

	bootstrapCmd := exec.Cmd{
		Path: scriptPath,
		Args: []string{scriptPath, "bootstrap", "-v"},
		Env: append(os.Environ(),
			"GOOS="+p.OS,
			"GOARCH="+p.Arch,
			"GOROOT="+goRoot),
		Dir:    scriptDir,
		Stdout: os.Stdout,
		Stderr: os.Stderr,
	}

	return bootstrapCmd.Run()
}
