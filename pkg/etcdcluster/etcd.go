package etcdcluster

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

type Client struct {
	URLs []string
}

func (c *Client) AddMember(url string) (*Member, error) {
	data, err := json.Marshal(map[string][]string{"peerURLs": {url}})
	if err != nil {
		return nil, err
	}
	for _, url := range c.URLs {
		var res *http.Response
		res, err = http.Post(url+"/v2/members", "application/json", bytes.NewReader(data))
		if err != nil {
			continue
		}
		if res.StatusCode != 201 && res.StatusCode != 409 {
			res.Body.Close()
			return nil, fmt.Errorf("etcd: unexpected status %d adding member", res.StatusCode)
		}
		member := &Member{}
		json.NewDecoder(res.Body).Decode(member)
		res.Body.Close()
		return member, nil
	}
	return nil, err
}

func (c *Client) RemoveMember(id string) error {
	var err error
	for _, url := range c.URLs {
		var req *http.Request
		req, err = http.NewRequest("DELETE", fmt.Sprintf("%s/v2/members/%s", url, id), nil)
		if err != nil {
			continue
		}
		var res *http.Response
		res, err = http.DefaultClient.Do(req)
		if err != nil {
			continue
		}
		res.Body.Close()
		if res.StatusCode != 204 && res.StatusCode != 200 {
			return fmt.Errorf("etcd: unexpected status %d removing member", res.StatusCode)
		}
		return nil
	}
	return err
}

var timeoutClient = &http.Client{Timeout: 1 * time.Second}

func (c *Client) GetMembers() ([]Member, error) {
	var err error
	for _, url := range c.URLs {
		var req *http.Request
		req, err = http.NewRequest("GET", url+"/v2/members", nil)
		if err != nil {
			continue
		}
		var res *http.Response
		res, err = timeoutClient.Do(req)
		if err != nil {
			continue
		}
		if res.StatusCode != 200 {
			return nil, fmt.Errorf("etcd: unexpected status %d getting members", res.StatusCode)
		}
		var data struct {
			Members []Member `json:"members"`
		}
		err = json.NewDecoder(res.Body).Decode(&data)
		res.Body.Close()
		return data.Members, err
	}
	return nil, err
}

type Member struct {
	ID         string   `json:"id"`
	Name       string   `json:"name"`
	PeerURLs   []string `json:"peerURLs"`
	ClientURLs []string `json:"clientURLs"`
}
