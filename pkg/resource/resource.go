package resource

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
)

type Resource struct {
	ID  string            `json:"id"`
	Env map[string]string `json:"env"`
}

func Provision(uri string, config []byte) (*Resource, error) {
	res, err := http.Post(uri, "application/json", bytes.NewBuffer(config))
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return nil, fmt.Errorf("resource: unexpected status code %d", res.StatusCode)
	}

	resource := &Resource{}
	if err := json.NewDecoder(res.Body).Decode(resource); err != nil {
		return nil, err
	}
	return resource, nil
}
