package main

import (
	"os"
)

var cmdLogs = &Command{
	Run:   runLogs,
	Usage: "logs job",
	Short: "get job logs",
	Long:  `Retrieve job logs from Flynn`,
}

func runLogs(cmd *Command, args []string) {
	if len(args) != 1 {
		panic("You must supply a job id")
	}

	must(Get(os.Stdout, "/apps/"+mustApp()+"/jobs/"+args[0]+"/logs"))
}
