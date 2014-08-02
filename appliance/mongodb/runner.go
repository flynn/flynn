package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"syscall"
	"time"

	"github.com/flynn/go-discoverd"
	"gopkg.in/mgo.v2"
	"gopkg.in/mgo.v2/bson"
)

var dataDir = flag.String("data", "/data", "mongo data directory")
var serviceName = flag.String("service", "mongo", "discoverd service name")
var addr = ":" + os.Getenv("PORT")

func init() {
	log.SetFlags(log.Lmicroseconds | log.Lshortfile)
}

func main() {
	flag.Parse()

	cmd, err := startMongod()
	if err != nil {
		log.Fatal(err)
	}
	sess := waitForMongod(time.Minute)

	fatal := func(err error) {
		discoverd.UnregisterAll()
		cmd.Process.Signal(os.Interrupt)
		log.Fatal(err)
	}

	set, err := discoverd.RegisterWithSet(*serviceName, addr, nil)
	if err != nil {
		fatal(err)
	}

	log.Println("Registered with service discovery.")
	var self *discoverd.Service
	leaders := set.Leaders()
	for l := range leaders {
		if l.Addr == set.SelfAddr() {
			go func() {
				for _ = range leaders {
				}
			}()
			self = l
			break
		}
	}

	log.Println("Promoted to leader.")
	coll := sess.DB("local").C("system.replset")
	n, err := coll.Count()
	log.Println("ASDF ASDF REPLSET COUNT:", n, err)
	if err != nil {
		fatal(err)
	}
	if n == 0 {
		conf := bson.M{"replSetInitiate": bson.M{
			"_id":     "rs0",
			"members": buildMembers(self, set.Services()),
		}}
		log.Printf("Creating replset config: %#v", conf)
		err = sess.Run(conf, &struct{}{})
		if err != nil {
			fatal(err)
		}
	}

	log.Println("Handling updates...")
	for update := range set.Watch(true) {
		conf := make(bson.M)
		coll.Find(nil).One(&conf)
		conf["version"] = conf["version"].(int) + 1
		conf["members"] = buildMembers(self, set.Services())
		log.Printf("Processing update: %#v, config: %#v", update, conf)
		if err := sess.Run(bson.M{"replSetReconfig": conf}, &struct{}{}); err != nil {
			fatal(err)
		}
	}
}

func waitExit(cmd *exec.Cmd) {
	cmd.Wait()
	discoverd.UnregisterAll()
	var status int
	if ws, ok := cmd.ProcessState.Sys().(syscall.WaitStatus); ok {
		status = ws.ExitStatus()
	}
	os.Exit(status)
}

func buildMembers(self *discoverd.Service, services []*discoverd.Service) []replMember {
	res := make([]replMember, len(services)+1)
	res[0].Host = self.Addr
	for i, s := range services {
		res[i+1].ID = uint8(i + 1)
		res[i+1].Host = s.Addr
	}
	return res
}

type replMember struct {
	ID   uint8  `bson:"_id"`
	Host string `bson:"host"`
}

func waitForMongod(maxWait time.Duration) *mgo.Session {
	log.Println("Waiting for mongo to boot...")
	start := time.Now()
	for {
		sess, err := mgo.Dial(fmt.Sprintf("mongodb://127.0.0.1:%s?connect=direct", os.Getenv("PORT")))
		if err != nil {
			if time.Now().Sub(start) >= maxWait {
				log.Fatalf("Unable to connect to mongo after %s, last error: %q", maxWait, err)
			}
			time.Sleep(100 * time.Millisecond)
			continue
		}
		sess.SetMode(mgo.Eventual, true)
		return sess
	}
}

func startMongod() (*exec.Cmd, error) {
	log.Println("Starting mongod...")

	cmd := exec.Command(
		"mongod",
		"--dbpath", *dataDir,
		"--port", os.Getenv("PORT"),
		"--replSet", "rs0",
		"--noauth",
		"-v",
	)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	go handleSignals(cmd)
	go waitExit(cmd)
	return cmd, nil
}

func handleSignals(cmd *exec.Cmd) {
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)

	sig := <-c
	discoverd.UnregisterAll()
	cmd.Process.Signal(sig)
}
