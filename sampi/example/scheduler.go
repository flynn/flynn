package main

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"encoding/gob"
	"io"
	"log"
	"net"
	"os"
	"time"

	"github.com/dotcloud/docker/term"
	"github.com/flynn/go-discover/discover"
	"github.com/flynn/lorne/types"
	"github.com/flynn/rpcplus"
	"github.com/flynn/sampi/types"
	"github.com/titanous/go-dockerclient"
)

func main() {
	disc, err := discover.NewClient()
	if err != nil {
		log.Fatal(err)
	}
	services := disc.Services("flynn-sampi")
	time.Sleep(100 * time.Millisecond) // HAX: remove this when Online is blocking
	schedulers := services.OnlineAddrs()
	if len(schedulers) == 0 {
		log.Fatal("No sampi instances found")
	}

	scheduler, err := rpcplus.DialHTTP("tcp", schedulers[0])
	if err != nil {
		log.Fatal(err)
	}

	var state map[string]sampi.Host
	if err := scheduler.Call("Scheduler.State", struct{}{}, &state); err != nil {
		log.Fatal(err)
	}

	var firstHost string
	for k := range state {
		firstHost = k
		break
	}
	if firstHost == "" {
		log.Fatal("no hosts")
	}

	id := randomID()

	services = disc.Services("flynn-lorne-attach." + firstHost)
	conn, err := net.Dial("tcp", services.OnlineAddrs()[0])
	if err != nil {
		log.Fatal(err)
	}
	ws, _ := term.GetWinsize(os.Stdin.Fd())
	err = gob.NewEncoder(conn).Encode(&lorne.AttachReq{
		JobID:  id,
		Flags:  lorne.AttachFlagStdout | lorne.AttachFlagStderr | lorne.AttachFlagStdin | lorne.AttachFlagStream,
		Height: int(ws.Height),
		Width:  int(ws.Width),
	})
	if err != nil {
		log.Fatal(err)
	}
	attachState := make([]byte, 1)
	if _, err := conn.Read(attachState); err != nil {
		log.Fatal(err)
	}
	switch attachState[0] {
	case lorne.AttachWaiting:
		log.Print("attach waiting")
	case lorne.AttachError:
		log.Fatal("attach error")
	}

	var schedRes sampi.ScheduleRes
	schedReq := &sampi.ScheduleReq{
		Incremental: true,
		HostJobs: map[string][]*sampi.Job{firstHost: {{ID: id, Config: &docker.Config{
			Image:        "titanous/redis",
			Cmd:          []string{"/redis/src/redis-cli", "-h", "10.0.2.15"},
			Tty:          true,
			AttachStdin:  true,
			AttachStdout: true,
			AttachStderr: true,
			OpenStdin:    true,
			StdinOnce:    true,
		}}}},
	}
	if err := scheduler.Call("Scheduler.Schedule", schedReq, &schedRes); err != nil {
		log.Fatal(err)
	}

	if _, err := conn.Read(attachState); err != nil {
		log.Fatal(err)
	}

	oldState, err := term.SetRawTerminal(os.Stdin.Fd())
	if err != nil {
		log.Fatal(err)
	}
	go io.Copy(conn, os.Stdin)
	if _, err := io.Copy(os.Stdout, conn); err != nil {
		log.Fatal(err)
	}
	term.RestoreTerminal(os.Stdin.Fd(), oldState)
}

func randomID() string {
	b := make([]byte, 16)
	enc := make([]byte, 24)
	_, err := io.ReadFull(rand.Reader, b)
	if err != nil {
		panic(err) // This shouldn't ever happen, right?
	}
	base64.URLEncoding.Encode(enc, b)
	return string(bytes.TrimRight(enc, "="))
}
