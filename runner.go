package main

import (
	"bytes"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"text/template"
	"time"

	da "github.com/flynn/discoverd/agent"
	"github.com/flynn/go-discoverd"
	_ "github.com/lib/pq"
)

var dataDir = flag.String("data", "/data", "postgresql data directory")
var serviceName = flag.String("service", "pg", "discoverd service name")
var pgbin = flag.String("pgbin", "/usr/lib/postgresql/9.3/bin/", "postgres binary directory")
var addr = ":" + os.Getenv("PORT")

func main() {
	flag.Parse()

	set, err := discoverd.RegisterWithSet(*serviceName, addr, nil)
	if err != nil {
		log.Fatal(err)
	}

	var username, password string
	var follower *follower
	var leaderProc *exec.Cmd
	var done <-chan struct{}
	var leader *discoverd.Service

	if l := set.Leader(); l.Addr == set.SelfAddr() {
		leaderProc, done = startLeader()
		goto wait
	}

	for u := range set.Watch(true) {
		l := set.Leader()
		if u.Online && u.Addr == l.Addr && u.Attrs["username"] != "" && u.Attrs["password"] != "" {
			username, password = u.Attrs["username"], u.Attrs["password"]
		}
		if leader != nil && l.Addr == leader.Addr {
			continue
		}
		leader = l
		if leader.Addr == set.SelfAddr() {
			leaderProc, done = promoteToLeader(follower, username, password)
			goto wait
		} else {
			if follower == nil {
				follower = startFollower(leader, set)
			} else {
				follower = switchLeader(leader, set, follower)
			}
		}
	}
	// TODO: handle service discovery disconnection

wait:
	set.Close()
	<-done
	procExit(leaderProc)
}

func startLeader() (*exec.Cmd, <-chan struct{}) {
	log.Println("Starting as leader...")
	if err := dirIsEmpty(*dataDir); err == nil {
		log.Println("Running initdb...")
		runCmd(exec.Command(
			filepath.Join(*pgbin, "initdb"),
			"-D", *dataDir,
			"--encoding=UTF-8",
			"--locale=en_US.UTF-8", // TODO: make this configurable?
		))
	} else if err != ErrNotEmpty {
		log.Fatal(err)
	}

	cmd, err := startPostgres(*dataDir)
	if err != nil {
		log.Fatal(err)
	}

	db := waitForPostgres(time.Minute)
	password := createSuperuser(db)
	db.Close()
	register(map[string]string{"username": "flynn", "password": password, "up": "true"})

	done := make(chan struct{})
	go func() {
		cmd.Wait()
		close(done)
	}()

	return cmd, done
}

func register(attrs map[string]string) {
	err := discoverd.RegisterWithAttributes(*serviceName, addr, attrs)
	if err != nil {
		log.Fatalln("discoverd registration error:", err)
	}
}

func procExit(cmd *exec.Cmd) {
	discoverd.UnregisterAll()
	var status int
	if ws, ok := cmd.ProcessState.Sys().(syscall.WaitStatus); ok {
		status = ws.ExitStatus()
	}
	os.Exit(status)
}

func createSuperuser(db *sql.DB) (password string) {
	log.Println("Creating superuser...")
	password = generatePassword()

	_, err := db.Exec("DROP USER IF EXISTS flynn")
	if err != nil {
		log.Fatalln("Error dropping user:", err)
	}
	_, err = db.Exec("CREATE USER flynn WITH SUPERUSER CREATEDB CREATEROLE REPLICATION PASSWORD '" + password + "'")
	if err != nil {
		log.Fatalln("Error creating user:", err)
	}
	log.Println("Superuser created.")

	return
}

func generatePassword() string {
	b := make([]byte, 16)
	enc := make([]byte, 24)
	_, err := io.ReadFull(rand.Reader, b)
	if err != nil {
		panic(err) // This shouldn't ever happen, right?
	}
	base64.URLEncoding.Encode(enc, b)
	return string(bytes.TrimRight(enc, "="))
}

var pgstr = "user=postgres host=/var/run/postgresql sslmode=disable port=" + os.Getenv("PORT")

func waitForPostgres(maxWait time.Duration) *sql.DB {
	log.Println("Waiting for postgres to boot...")
	start := time.Now()
	for {
		var ping string
		db, err := sql.Open("postgres", pgstr)
		if err != nil {
			goto fail
		}
		err = db.QueryRow("SELECT 'ping'").Scan(&ping)
		if ping == "ping" {
			log.Println("Postgres is up.")
			return db
		}
		db.Close()

	fail:
		if time.Now().Sub(start) >= maxWait {
			log.Fatalf("Unable to connect to postgres after %s, last error: %q", maxWait, err)
		}
		time.Sleep(time.Second)
	}
}

func waitForPromotion() {
	log.Println("Waiting for promotion...")
	db, err := sql.Open("postgres", pgstr)
	if err != nil {
		log.Fatalln("Error connecting to postgres:", err)
	}
	defer db.Close()
	for {
		var recovery bool
		err := db.QueryRow("SELECT pg_is_in_recovery()").Scan(&recovery)
		if err != nil {
			log.Fatalln("Error checking recovery status:", err)
		}
		if !recovery {
			return
		}
		time.Sleep(time.Second)
	}
}

func promoteToLeader(follower *follower, username, password string) (*exec.Cmd, <-chan struct{}) {
	log.Println("Promoting follower to leader...")
	register(map[string]string{"up": "false"})
	f, err := os.Create(filepath.Join(*dataDir, "promote.trigger"))
	if err != nil {
		panic(err)
	}
	f.Close()

	waitForPromotion()

	if username == "" || password == "" {
		// TODO: create superuser
	}

	register(map[string]string{"up": "true", "username": username, "password": password})
	log.Println("Follower promoted to leader.")
	return follower.Cancel()
}

func runCmd(cmd *exec.Cmd) {
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			if status, ok := exitErr.Sys().(syscall.WaitStatus); ok {
				os.Exit(status.ExitStatus())
			}
		}
		log.Fatal(err)
	}
}

func pullBaseBackup(s *discoverd.Service) {
	log.Println("Running pg_basebackup...")
	runCmd(exec.Command(
		"pg_basebackup",
		"-D", *dataDir,
		"-d", fmt.Sprintf("host=%s port=%s user=%s password=%s", s.Host, s.Port, s.Attrs["username"], s.Attrs["password"]),
		"--xlog-method=stream",
		"--progress",
		"--verbose",
	))
	log.Println("pg_basebackup complete.")
}

var recoveryTempl = template.Must(template.New("recovery").Parse(`
standby_mode = 'on'
primary_conninfo = 'host={{.Host}} port={{.Port}} user={{.Username}} password={{.Password}}'
trigger_file = '{{.Trigger}}'
`))

type recoveryConfig struct {
	Host     string
	Port     string
	Username string
	Password string
	Trigger  string
}

func writeRecoveryConf(dir string, leader *discoverd.Service) {
	f, err := os.Create(filepath.Join(dir, "recovery.conf"))
	if err != nil {
		log.Fatalln("Error creating recovery.conf:", err)
	}
	defer f.Close()

	err = recoveryTempl.Execute(f, &recoveryConfig{
		Host:     leader.Host,
		Port:     leader.Port,
		Username: leader.Attrs["username"],
		Password: leader.Attrs["password"],
		Trigger:  filepath.Join(dir, "promote.trigger"),
	})
	if err != nil {
		log.Fatalln("Error writing recovery.conf:", err)
	}
}

func updateToService(u *da.ServiceUpdate) *discoverd.Service {
	host, port, _ := net.SplitHostPort(u.Addr)
	return &discoverd.Service{
		Created: u.Created,
		Name:    u.Name,
		Addr:    u.Addr,
		Attrs:   u.Attrs,
		Host:    host,
		Port:    port,
	}
}

func waitForLeaderUp(leader *discoverd.Service, set discoverd.ServiceSet) *discoverd.Service {
	if leader.Attrs["up"] == "true" {
		return leader
	}
	log.Println("Waiting for leader to come up...")
	watch := set.Watch(true)
	defer set.Unwatch(watch)
	for update := range watch {
		if update.Addr == set.Leader().Addr && update.Attrs["up"] == "true" && update.Attrs["username"] != "" && update.Attrs["password"] != "" {
			return updateToService(update)
		}
	}
	return nil
}

func startFollower(leader *discoverd.Service, set discoverd.ServiceSet) *follower {
	log.Println("Starting as follower...")
	leader = waitForLeaderUp(leader, set)
	if err := dirIsEmpty(*dataDir); err == nil {
		pullBaseBackup(leader)
	} else if err != ErrNotEmpty {
		log.Fatal(err)
	}

	writeRecoveryConf(*dataDir, leader)
	cmd, err := startPostgres(*dataDir)
	if err != nil {
		log.Fatal(err)
	}

	waitForPostgres(time.Minute).Close()
	register(map[string]string{"up": "true"})
	log.Println("Follower started.")

	// TODO: if data and insufficient WAL, pg_basebackup
	return newFollower(cmd)
}

func switchLeader(leader *discoverd.Service, set discoverd.ServiceSet, follower *follower) *follower {
	log.Println("Switching leaders...")
	leader = waitForLeaderUp(leader, set)
	register(map[string]string{"up": "false"})
	writeRecoveryConf(*dataDir, leader)
	follower.Stop()

	cmd, err := startPostgres(*dataDir)
	if err != nil {
		log.Fatal(err)
	}
	waitForPostgres(time.Minute).Close()
	// TODO: check for insufficient WAL, then pg_basebackup
	register(map[string]string{"up": "true"})
	log.Println("Leader switch complete.")
	return newFollower(cmd)
}

func newFollower(cmd *exec.Cmd) *follower {
	f := &follower{
		cmd:  cmd,
		stop: make(chan struct{}),
		done: make(chan struct{}),
	}
	go f.wait()
	return f
}

type follower struct {
	cmd  *exec.Cmd
	stop chan struct{}
	done chan struct{}
}

func (f *follower) wait() {
	go func() {
		f.cmd.Wait()
		close(f.done)
	}()

	select {
	case <-f.done:
		procExit(f.cmd)
	case <-f.stop:
	}
}

func (f *follower) Cancel() (*exec.Cmd, <-chan struct{}) {
	close(f.stop)
	return f.cmd, f.done
}

func (f *follower) Stop() error {
	close(f.stop)
	if err := f.cmd.Process.Signal(syscall.SIGTERM); err != nil {
		return err
	}
	// TODO: escalate to kill?
	<-f.done
	return nil
}

func writeConfig(dataDir string) {
	err := copyFile("/etc/postgresql/9.3/main/postgresql.conf", filepath.Join(dataDir, "postgresql.conf"))
	if err != nil {
		log.Fatalln("Error creating postgresql.conf", err)
	}

	err = copyFile("/etc/postgresql/9.3/main/pg_hba.conf", filepath.Join(dataDir, "pg_hba.conf"))
	if err != nil {
		log.Fatalln("Error creating pg_hba.conf", err)
	}

	err = writeCert(os.Getenv("EXTERNAL_IP"), dataDir)
	if err != nil {
		log.Fatalln("Error writing ssl info", err)
	}
}

func copyFile(src, dest string) error {
	sf, err := os.Open(src)
	if err != nil {
		return err
	}
	defer sf.Close()
	df, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer df.Close()

	_, err = io.Copy(df, sf)
	return err
}

func startPostgres(dataDir string) (*exec.Cmd, error) {
	writeConfig(dataDir)

	log.Println("Starting postgres...")
	cmd := exec.Command(
		filepath.Join(*pgbin, "postgres"),
		"-D", dataDir, // Set datadir
		"-p", os.Getenv("PORT"), // Set port to $PORT
		"-h", "*", // Listen on all interfaces
		"-l", // Enable SSL
	)
	log.Println("exec", strings.Join(cmd.Args, " "))
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	go handleSignals(cmd)
	return cmd, nil
}

func handleSignals(cmd *exec.Cmd) {
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM, syscall.SIGQUIT)

	sig := <-c
	discoverd.UnregisterAll()
	cmd.Process.Signal(sig)
}

var ErrNotEmpty = errors.New("directory is not empty")

func dirIsEmpty(dir string) error {
	d, err := os.Open(dir)
	if err != nil {
		if errno, ok := err.(syscall.Errno); ok && errno == syscall.ENOENT {
			return nil
		}
		return err
	}
	defer d.Close()

	for {
		fs, err := d.Readdir(10)
		if err != nil {
			if err == io.EOF {
				break
			}
			return err
		}
		for _, f := range fs {
			if !strings.HasPrefix(f.Name(), ".") {
				return ErrNotEmpty
			}
		}
	}

	return nil
}
