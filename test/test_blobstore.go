package main

import (
	"bytes"
	"io"
	"net/http"
	"time"

	c "github.com/flynn/flynn/Godeps/_workspace/src/github.com/flynn/go-check"
	"github.com/flynn/flynn/discoverd/client"
)

type BlobstoreSuite struct{}

var _ = c.Suite(&BlobstoreSuite{})

// Transfer >512MB data to avoid regressing on https://github.com/flynn/flynn/issues/101
func (b *BlobstoreSuite) TestLargeAmountOfData(t *c.C) {
	disc, err := discoverd.NewClientWithAddr(routerIP + ":1111")
	t.Assert(err, c.IsNil)
	defer disc.Close()

	services, err := disc.Services("blobstore", 5*time.Second)
	t.Assert(err, c.IsNil)

	path := "http://" + services[0].Addr + "/data"
	data := make([]byte, 16*1024*1024)

	for i := 0; i < 17; i++ {
		req, err := http.NewRequest("PUT", path, bytes.NewReader(data))
		t.Assert(err, c.IsNil)
		res, err := http.DefaultClient.Do(req)
		t.Assert(err, c.IsNil)
		t.Assert(res.StatusCode, c.Equals, http.StatusOK)

		res, err = http.Get(path)
		t.Assert(err, c.IsNil)
		_, err = io.ReadFull(res.Body, data)
		res.Body.Close()
		t.Assert(err, c.IsNil)
	}
}
