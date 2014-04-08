package bootstrap

import (
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/flynn/go-discoverd"
	"github.com/flynn/go-discoverd/dialer"
)

type WaitAction struct {
	URL string `json:"url"`
}

func init() {
	Register("wait", &WaitAction{})
}

func (a *WaitAction) Run(s *State) error {
	const waitMax = time.Minute
	const waitInterval = 500 * time.Millisecond

	u, err := url.Parse(a.URL)
	if err != nil {
		return err
	}
	httpc := http.DefaultClient
	if u.Scheme == "discoverd+http" {
		if err := discoverd.Connect(""); err != nil {
			return err
		}
		d := dialer.New(discoverd.DefaultClient, nil)
		defer d.Close()
		httpc = &http.Client{Transport: &http.Transport{Dial: d.Dial}}
		u.Scheme = "http"
	}

	start := time.Now()
	for {
		var result string
		res, err := httpc.Get(u.String())
		if err != nil {
			result = fmt.Sprintf("%q", err)
			goto fail
		}
		res.Body.Close()
		if res.StatusCode == 200 {
			return nil
		}
		result = strconv.Itoa(res.StatusCode)

	fail:
		if time.Now().Sub(start) >= waitMax {
			return fmt.Errorf("bootstrap: timed out waiting for %s, last response %s", a.URL, result)
		}
		time.Sleep(waitInterval)
	}
}

func (a *WaitAction) Cleanup(s *State) error { return nil }
