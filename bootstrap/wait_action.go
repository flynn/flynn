package bootstrap

import (
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/flynn/flynn/discoverd/client"
	"github.com/flynn/flynn/discoverd/client/dialer"
)

type WaitAction struct {
	URL    string `json:"url"`
	Host   string `json:"host"`
	Status int    `json:"status"`
}

func init() {
	Register("wait", &WaitAction{})
}

func (a *WaitAction) Run(s *State) error {
	const waitMax = time.Minute
	const waitInterval = 500 * time.Millisecond

	if a.Status == 0 {
		a.Status = 200
	}

	u, err := url.Parse(interpolate(s, a.URL))
	if err != nil {
		return err
	}
	httpc := http.DefaultClient
	var dial dialer.DialFunc
	if u.Scheme == "tcp" {
		dial = net.Dial
	}
	if strings.HasPrefix(u.Scheme, "discoverd+") {
		if err := discoverd.Connect(""); err != nil {
			return err
		}
		d := dialer.New(discoverd.DefaultClient, nil)
		defer d.Close()
		dial = d.Dial

		switch u.Scheme {
		case "discoverd+http":
			httpc = &http.Client{Transport: &http.Transport{Dial: d.Dial}}
			u.Scheme = "http"
		case "discoverd+tcp":
			u.Scheme = "tcp"
		default:
			return fmt.Errorf("bootstrap: unknown protocol")
		}
	}

	start := time.Now()
	for {
		var result string

		switch u.Scheme {
		case "http":
			req, err := http.NewRequest("GET", u.String(), nil)
			if err != nil {
				return err
			}
			if a.Host != "" {
				req.Host = interpolate(s, a.Host)
			}
			res, err := httpc.Do(req)
			if err != nil {
				result = fmt.Sprintf("%q", err)
				goto fail
			}
			res.Body.Close()
			if res.StatusCode == a.Status {
				return nil
			}
			result = strconv.Itoa(res.StatusCode)
		case "tcp":
			conn, err := dial("tcp", u.Host)
			if err != nil {
				result = fmt.Sprintf("%q", err)
				goto fail
			}
			conn.Close()
			return nil
		default:
			return fmt.Errorf("bootstrap: unknown protocol")
		}
	fail:
		if time.Now().Sub(start) >= waitMax {
			return fmt.Errorf("bootstrap: timed out waiting for %s, last response %s", a.URL, result)
		}
		time.Sleep(waitInterval)
	}
}
