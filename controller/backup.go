package main

import (
	"fmt"
	"net/http"
	"time"

	"github.com/flynn/flynn/Godeps/_workspace/src/golang.org/x/net/context"
	"github.com/flynn/flynn/controller/client"
	"github.com/flynn/flynn/pkg/backup"
)

func (c *controllerAPI) GetBackup(ctx context.Context, w http.ResponseWriter, req *http.Request) {
	w.Header().Set("Content-Type", "application/tar")
	filename := "flynn-backup-" + time.Now().UTC().Format("2006-01-02_150405") + ".tar"
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filename))
	client, err := controller.NewClient("", c.config.keys[0])
	if err != nil {
		respondWithError(w, err)
		return
	}
	if err := backup.Run(client, w, nil); err != nil {
		respondWithError(w, err)
		return
	}
}
