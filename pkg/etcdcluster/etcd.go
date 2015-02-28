package etcdcluster

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"path"
	"strings"
)

type Client struct {
	URLs []string
}

func (c *Client) AddMember(url string) error {
	data, err := json.Marshal(map[string][]string{"peerURLs": {url}})
	if err != nil {
		return err
	}
	for _, url := range c.URLs {
		var res *http.Response
		res, err = http.Post(url+"/v2/members", "application/json", bytes.NewReader(data))
		if err != nil {
			continue
		}
		res.Body.Close()
		if res.StatusCode != 201 && res.StatusCode != 409 {
			return fmt.Errorf("etcd: unexpected status %d adding member", res.StatusCode)
		}
		return nil
	}
	return err
}

func (c *Client) GetMembers() ([]Member, error) {
	var err error
	for _, url := range c.URLs {
		var res *http.Response
		res, err = http.Get(url + "/v2/members")
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

func Discover(url string) ([]Member, error) {
	res, err := http.Get(url)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return nil, fmt.Errorf("etcd: unexpected status %d during discovery", res.StatusCode)
	}

	var data struct {
		Node struct {
			Nodes []struct {
				Key   string `json:"key"`
				Value string `json:"value"`
			} `json:"nodes"`
		} `json:"node"`
	}
	if err := json.NewDecoder(res.Body).Decode(&data); err != nil {
		return nil, err
	}

	members := make([]Member, len(data.Node.Nodes))
	for i, n := range data.Node.Nodes {
		nameAddr := strings.SplitN(n.Value, "=", 2)
		if len(nameAddr) != 2 {
			return nil, fmt.Errorf("etcd: malformed value %q during discovery", n.Value)
		}
		members[i].Name = nameAddr[0]
		members[i].PeerURLs = []string{nameAddr[1]}
		members[i].ID = path.Base(n.Key)
	}

	return members, nil
}

func NewDiscoveryToken(size string) (string, error) {
	res, err := http.Get("https://discovery.etcd.io/new?size=" + size)
	if err != nil {
		return "", err
	}
	if res.StatusCode != 200 {
		return "", fmt.Errorf("error creating discovery token, got status %d", res.StatusCode)
	}
	defer res.Body.Close()
	url, err := ioutil.ReadAll(res.Body)
	return string(url), err
}
