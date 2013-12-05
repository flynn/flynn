package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/flynn/sampi/types"
)

func openConfig(file string) (*sampi.Host, error) {
	f, err := os.Open(file)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return parseConfig(f)
}

func parseConfig(r io.Reader) (*sampi.Host, error) {
	var conf Config
	if err := json.NewDecoder(r).Decode(&conf); err != nil {
		return nil, err
	}
	return conf.hostConfig()
}

type Config struct {
	Resources  map[string]sampi.ResourceValue `json:"resources"`
	Attributes map[string]string              `json:"attributes"`
	Rules      []Rule                         `json:"rules"`
}

func (c *Config) hostConfig() (*sampi.Host, error) {
	host := &sampi.Host{Resources: c.Resources, Attributes: c.Attributes}
	host.Rules = make([]sampi.Rule, len(c.Rules))
	for i, r := range c.Rules {
		rule := sampi.Rule{Key: r.Key, Value: r.Value}
		switch r.Op {
		case "==":
			rule.Op = sampi.OpEq
		case "!=":
			rule.Op = sampi.OpNotEq
		case ">":
			rule.Op = sampi.OpGt
		case ">=":
			rule.Op = sampi.OpGtEq
		case "<":
			rule.Op = sampi.OpLt
		case "<=":
			rule.Op = sampi.OpLtEq
		default:
			return nil, fmt.Errorf("lorne: invalid rule op: %s", r.Op)
		}
		host.Rules[i] = rule
	}
	return host, nil
}

type Rule struct {
	Key   string `json:"key"`
	Op    string `json:"op"`
	Value string `json:"value"`
}
