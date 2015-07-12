package mounts

import (
	"bufio"
	"fmt"
	"os"
	"strings"
)

type Mount struct {
	Device          string
	Mountpoint      string
	MountpointDepth int
	Type            string
	Flags           string
}

func ParseFile(name string) ([]Mount, error) {
	f, err := os.Open(name)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	var res []Mount
	for scanner.Scan() {
		line := scanner.Text()
		fields := strings.SplitN(line, " ", 5)
		if len(fields) != 5 {
			return nil, fmt.Errorf("mounts: error parsing line: %q", line)
		}
		res = append(res, Mount{
			Device:          fields[0],
			Mountpoint:      fields[1],
			MountpointDepth: strings.Count(fields[1], "/"),
			Type:            fields[2],
			Flags:           fields[3],
		})
	}
	return res, scanner.Err()
}

type ByDepth []Mount

func (p ByDepth) Len() int           { return len(p) }
func (p ByDepth) Less(i, j int) bool { return p[i].MountpointDepth > p[j].MountpointDepth }
func (p ByDepth) Swap(i, j int)      { p[i], p[j] = p[j], p[i] }
