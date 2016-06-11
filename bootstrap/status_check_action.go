package bootstrap

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"
)

type StatusCheckAction struct {
	ID      string `json:"id"`
	URL     string `json:"url"`
	Output  string `json:"output"`
	Timeout int    `json:"timeout"` // in seconds
}

type StatusResponse struct {
	Data StatusData `json:"data"`
}

type StatusData struct {
	Status string                  `json:"status"`
	Detail map[string]StatusDetail `json:"detail"`
}

type StatusDetail struct {
	Status string `json:"status"`
}

func init() {
	Register("status-check", &StatusCheckAction{})
}

func (a *StatusCheckAction) Run(s *State) error {
	waitMax := time.Minute
	if a.Timeout > 0 {
		waitMax = time.Duration(a.Timeout) * time.Second
	}
	const waitInterval = 500 * time.Millisecond

	u, err := url.Parse(interpolate(s, a.URL))
	if err != nil {
		return err
	}
	lookupDiscoverdURLHost(s, u, waitMax)

	timeout := time.After(waitMax)
	for {
		req, err := http.NewRequest("GET", u.String(), nil)
		if err != nil {
			return err
		}
		req.Header = make(http.Header)
		req.Header.Set("Accept", "application/json")
		res, err := http.DefaultClient.Do(req)
		if err == nil && res.StatusCode == 200 {
			res.Body.Close()
			s.StepData[a.ID] = &LogMessage{Msg: "all services healthy"}
			return nil
		}
		var status StatusResponse
		if err == nil {
			err = json.NewDecoder(res.Body).Decode(&status)
			res.Body.Close()
			if err != nil {
				return err
			}
		}

		select {
		case <-time.After(waitInterval):
			continue
		case <-timeout:
		}

		if err != nil {
			return fmt.Errorf("bootstrap: timed out waiting for %s, last response %s", a.URL, err)
		}

		msg := "unhealthy services detected!\n\nThe following services are reporting unhealthy, this likely indicates a problem with your deployment:\n"
		for svc, s := range status.Data.Detail {
			if s.Status != "healthy" {
				msg += "\t" + svc + "\n"
			}
		}
		msg += "\n"
		s.StepData[a.ID] = &LogMessage{Msg: msg}
		return nil
	}
}
