package main

import (
	"flag"
	"log"
	"os"
	"sync"
	"sync/atomic"

	"github.com/flynn/flynn/blobstore/data"
	"github.com/flynn/flynn/pkg/postgres"
)

func main() {
	deleteFiles := flag.Bool("delete", false, "enable deletion of files from source backend")
	concurrency := flag.Int("concurrency", 4, "number of parallel file moves to run at a time")
	prefix := flag.String("prefix", "", "only migrate files with a name that starts with this prefix")
	flag.Parse()

	db := postgres.Wait(nil, nil)
	repo, err := data.NewFileRepoFromEnv(db)
	if err != nil {
		log.Fatal(err)
	}

	files, err := repo.ListFilesExcludingDefaultBackend(*prefix)
	if err != nil {
		log.Fatal(err)
	}

	var wg sync.WaitGroup
	tokens := make(chan struct{}, *concurrency)
	var errorCount int64

	dest := repo.DefaultBackend().Name()
	for i, f := range files {
		log.Printf("[%d/%d] Moving %s (%s, %d bytes) from %s to %s", i+1, len(files), f.FileInfo.Name, f.ID, f.Size, f.Backend.Name(), dest)
		tokens <- struct{}{}
		wg.Add(1)
		go func(f data.BackendFile) {
			if err := moveFile(db, repo, f, *deleteFiles); err != nil {
				log.Printf("Error moving %s (%s): %s", f.FileInfo.Name, f.ID, err)
				atomic.AddInt64(&errorCount, 1)
			}
			<-tokens
			wg.Done()
		}(f)
	}

	wg.Wait()
	db.Close()
	if errorCount > 0 {
		log.Printf("Finished with %d errors", errorCount)
		os.Exit(1)
	} else {
		log.Printf("Finished with no errors.")
	}
}

func moveFile(db *postgres.DB, repo *data.FileRepo, f data.BackendFile, delete bool) error {
	b := repo.DefaultBackend()
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	stream, err := f.Backend.Open(tx, f.FileInfo, false)
	if err != nil {
		tx.Rollback()
		return err
	}
	if err := b.Put(tx, f.FileInfo, stream, false); err != nil {
		tx.Rollback()
		return err
	}
	if err := repo.SetBackend(tx, f.ID, b.Name()); err != nil {
		tx.Rollback()
		return err
	}
	if delete {
		if err := f.Backend.Delete(tx, f.FileInfo); err != nil {
			// print but don't return error if deletion of old file fails, we don't want to lose files
			log.Printf("Error deleting %s (%s) from %s: %s", f.FileInfo.Name, f.ID, f.Backend.Name(), err)
		}
	}
	return tx.Commit()
}
