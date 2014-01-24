package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/flynn/flynn-host/types"
)

func openConfig(file string) (*host.Host, error) {
	f, err := os.Open(file)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return parseConfig(f)
}

func parseConfig(r io.Reader) (*host.Host, error) {
	var conf Config
	if err := json.NewDecoder(r).Decode(&conf); err != nil {
		return nil, err
	}
	return conf.hostConfig()
}

type Config struct {
	Resources  map[string]host.ResourceValue `json:"resources"`
	Attributes map[string]string             `json:"attributes"`
	Rules      []Rule                        `json:"rules"`
}

func (c *Config) hostConfig() (*host.Host, error) {
	h := &host.Host{Resources: c.Resources, Attributes: c.Attributes}
	h.Rules = make([]host.Rule, len(c.Rules))
	for i, r := range c.Rules {
		rule := host.Rule{Key: r.Key, Value: r.Value}
		switch r.Op {
		case "==":
			rule.Op = host.OpEq
		case "!=":
			rule.Op = host.OpNotEq
		case ">":
			rule.Op = host.OpGt
		case ">=":
			rule.Op = host.OpGtEq
		case "<":
			rule.Op = host.OpLt
		case "<=":
			rule.Op = host.OpLtEq
		default:
			return nil, fmt.Errorf("lorne: invalid rule op: %s", r.Op)
		}
		h.Rules[i] = rule
	}
	return h, nil
}

type Rule struct {
	Key   string `json:"key"`
	Op    string `json:"op"`
	Value string `json:"value"`
}
