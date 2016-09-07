package main

import (
	"flag"
	"log"
	"sync"

	"github.com/flynn/flynn/blobstore/data"
	"github.com/flynn/flynn/pkg/postgres"
)

func main() {
	concurrency := flag.Int("concurrency", 4, "number of parallel file deletions to run at a time")
	flag.Parse()

	db := postgres.Wait(nil, nil)
	repo, err := data.NewFileRepoFromEnv(db)
	if err != nil {
		log.Fatal(err)
	}

	files, err := repo.ListDeletedFilesForCleanup()
	if err != nil {
		log.Fatal(err)
	}

	var wg sync.WaitGroup
	tokens := make(chan struct{}, *concurrency)
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
}
