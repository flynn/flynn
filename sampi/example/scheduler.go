package main

import (
	"log"

	"github.com/flynn/rpcplus"
	"github.com/flynn/sampi/types"
	"github.com/titanous/go-dockerclient"
)

func main() {
	scheduler, err := rpcplus.DialHTTP("tcp", "localhost:1112")
	if err != nil {
		log.Fatal(err)
	}

	var state map[string]types.Host
	if err := scheduler.Call("Scheduler.State", struct{}{}, &state); err != nil {
		log.Fatal(err)
	}
	log.Printf("%#v", state)

	var firstHost string
	for k := range state {
		firstHost = k
		break
	}
	if firstHost == "" {
		log.Fatal("no hosts")
	}

	var schedRes types.ScheduleRes
	schedReq := &types.ScheduleReq{
		Incremental: true,
		HostJobs:    map[string][]*types.Job{firstHost: {{ID: "test", Config: &docker.Config{Image: "crosbymichael/redis"}}}},
	}
	if err := scheduler.Call("Scheduler.Schedule", schedReq, &schedRes); err != nil {
		log.Fatal(err)
	}
	log.Print("scheduled container")
}
