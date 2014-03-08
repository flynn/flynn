package utils

import (
	"errors"
	"net/url"
)

func FormatEnv(envs ...map[string]string) []string {
	env := make(map[string]string)
	for _, e := range envs {
		for k, v := range e {
			env[k] = v
		}
	}
	res := make([]string, 0, len(env))
	for k, v := range env {
		res = append(res, k+"="+v)
	}
	return res
}

func DockerImage(uri string) (string, error) {
	// TODO: ID refs (see https://github.com/dotcloud/docker/issues/4106)
	u, err := url.Parse(uri)
	if err != nil {
		return "", err
	}
	if u.Scheme != "docker" {
		return "", errors.New("utils: only docker artifact URIs are currently supported")
	}
	var suffix string
	if tag := u.Query().Get("tag"); tag != "" {
		suffix = ":" + tag
	}
	return u.Host + u.Path + suffix, nil
}
