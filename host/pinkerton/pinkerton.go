package pinkerton

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os/exec"
)

type LayerPullInfo struct {
	ID     string
	Status string
}

func Pull(url string) ([]LayerPullInfo, error) {
	var layers []LayerPullInfo
	var errBuf bytes.Buffer
	cmd := exec.Command("pinkerton", "pull", "--json", url)
	stdout, _ := cmd.StdoutPipe()
	cmd.Stderr = &errBuf
	cmd.Start()
	j := json.NewDecoder(stdout)
	for {
		var l LayerPullInfo
		if err := j.Decode(&l); err != nil {
			if err == io.EOF {
				break
			}
			go cmd.Wait()
			return nil, err
		}
		layers = append(layers, l)
	}
	if err := cmd.Wait(); err != nil {
		return nil, &Error{Output: errBuf.String(), Err: err}
	}
	return layers, nil
}

type Error struct {
	Output string
	Err    error
}

func (e *Error) Error() string {
	return fmt.Sprintf("pinkerton: %s - %q", e.Err, e.Output)
}

func Checkout(id, image string) (string, error) {
	var errBuf bytes.Buffer
	cmd := exec.Command("pinkerton", "checkout", id, image)
	cmd.Stderr = &errBuf
	path, err := cmd.Output()
	if err != nil {
		return "", &Error{Output: errBuf.String(), Err: err}
	}
	return string(bytes.TrimSpace(path)), nil
}

func Cleanup(id string) error {
	var errBuf bytes.Buffer
	cmd := exec.Command("pinkerton", "cleanup", id)
	cmd.Stderr = &errBuf
	if err := cmd.Run(); err != nil {
		return &Error{Output: errBuf.String(), Err: err}
	}
	return nil
}

var ErrNoImageID = errors.New("pinkerton: missing image id")

func ImageID(s string) (string, error) {
	u, err := url.Parse(s)
	if err != nil {
		return "", err
	}
	q := u.Query()
	id := q.Get("id")
	if id == "" {
		return "", ErrNoImageID
	}
	return id, nil
}
