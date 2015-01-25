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
	lookupDiscoverdURLHost(u, waitMax)

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
			res, err := http.DefaultClient.Do(req)
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
			conn, err := net.Dial("tcp", u.Host)
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

func lookupDiscoverdURLHost(u *url.URL, timeout time.Duration) error {
	if strings.HasSuffix(u.Host, ".discoverd") {
		instances, err := discoverd.GetInstances(strings.Split(u.Host, ".")[0], timeout)
		if err != nil {
			return err
		}
		u.Host = instances[0].Addr
	}
	return nil
}
