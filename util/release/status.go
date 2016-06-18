package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"

	"github.com/flynn/go-docopt"
)

type Status struct {
	State string `json:"state"`
	Count int    `json:"total_count"`
}

func status(args *docopt.Args) {
	commit := args.String["<commit>"]
	res, err := http.Get(fmt.Sprintf("https://api.github.com/repos/flynn/flynn/commits/%s/status", commit))
	if err != nil {
		log.Fatal("error getting status from Github:", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		log.Fatalf("expected %d status code from Github, got %d", http.StatusOK, res.StatusCode)
	}
	status := &Status{}
	if err := json.NewDecoder(res.Body).Decode(status); err != nil {
		log.Fatal("error decoding Github response:", err)
	}
	if status.Count < 1 {
		log.Fatalf("commit does not have enough statuses (expected 1, got %d)", status.Count)
	}
	fmt.Println(status.State)
	if status.State != "success" {
		os.Exit(1)
	}
}
