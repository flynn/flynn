package main

import (
	"log"
	"strconv"
	"sync"

	"github.com/flynn/flynn/blobstore/data"
	"github.com/flynn/flynn/pkg/postgres"
	docopt "github.com/flynn/go-docopt"
)

func init() {
	register("cleanup", runCleanup, `
usage: flynn-blobstore cleanup [-c <concurrency>]

Delete file blobs that were already moved to a different backend from the default backend. 

Options:
     -c, --concurrency=<concurrency>  number of parallel file deletions to run at a time. [default: 4]
`)
}

func runCleanup(args *docopt.Args) error {
	concurrency, err := strconv.Atoi(args.String["--concurrency"])
	if err != nil {
		return err
	}
	if concurrency < 1 {
		concurrency = 4
	}

	db := postgres.Wait(nil, nil)
	repo, err := data.NewFileRepoFromEnv(db)
	if err != nil {
		return err
	}

	files, err := repo.ListDeletedFilesForCleanup()
	if err != nil {
		return err
	}

	var wg sync.WaitGroup
	tokens := make(chan struct{}, concurrency)
	for i, f := range files {
		if f.Backend == nil {
			log.Printf("[%d/%d] Skipping %s (%s) because backend is not configured", i+1, len(files), f.FileInfo.Name, f.ID)
			continue
		}
		tokens <- struct{}{}
		wg.Add(1)
		go func(f data.BackendFile) {
			if err := f.Backend.Delete(nil, f.FileInfo); err == nil {
				log.Printf("[%d/%d] Successfully deleted %s (%s) from backend %s", i+1, len(files), f.FileInfo.Name, f.ID, f.Backend.Name())
			}
			<-tokens
			wg.Done()
		}(f)
	}

	wg.Wait()
	db.Close()

	log.Printf("Done.")
	return nil
}
