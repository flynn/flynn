package main

import (
	"fmt"
	"sort"
	"strings"
)

var cmdPs = &Command{
	Run:   runPs,
	Usage: "ps",
	Short: "list jobs",
	Long:  `Lists jobs.`,
}

type Job struct {
	ID   string `json:"id"`
	Type string `json:"type"`
}

func runPs(cmd *Command, names []string) {
	var jobs []Job
	app := mustApp()
	must(Get(&jobs, "/apps/"+app+"/jobs"))

	if len(jobs) == 0 {
		return
	}

	ids := make([]string, len(jobs))
	for i, job := range jobs {
		ids[i] = job.ID[len(app)+1:]
	}
	sort.Strings(ids)
	fmt.Println(strings.Join(ids, "\n"))
}
