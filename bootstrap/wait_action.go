package bootstrap

import (
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
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
	const waitMax = 5 * time.Minute
	const waitInterval = 500 * time.Millisecond

	if a.Status == 0 {
		a.Status = 200
	}

	u, err := url.Parse(interpolate(s, a.URL))
	if err != nil {
		return err
	}
	if err := lookupDiscoverdURLHost(s, u, waitMax); err != nil {
		return err
	}

	timeout := time.After(waitMax)
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
		select {
		case <-timeout:
			return fmt.Errorf("bootstrap: timed out waiting for %s, last response %s", a.URL, result)
		case <-time.After(waitInterval):
		}
	}
}

func lookupDiscoverdURLHost(s *State, u *url.URL, timeout time.Duration) error {
	if strings.HasSuffix(u.Host, ".discoverd") {
		d, err := s.DiscoverdClient()
		if err != nil {
			return err
		}
		instances, err := d.Instances(strings.Split(u.Host, ".")[0], timeout)
		if err != nil {
			return err
		}
		u.Host = instances[0].Addr
	}
	return nil
}
