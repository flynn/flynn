package zfs

import (
	"os"
	"reflect"
	"sort"

	. "github.com/flynn/flynn/Godeps/_workspace/src/github.com/flynn/go-check"
)

type dirContainsChecker struct {
	*CheckerInfo
}

/*
	Asserts that the named directory exists, is a directory, and contains the provided list of
	files (or subdirectories; no distinction is made; just the names are checked).
*/
var DirContains Checker = &dirContainsChecker{
	&CheckerInfo{Name: "DirContains", Params: []string{"directory", "list"}},
}

func (checker *dirContainsChecker) Check(params []interface{}, names []string) (bool, string) {
	dirPath, ok := params[0].(string)
	if !ok {
		return false, "directory must be a string"
	}

	var fileList []string
	switch reflect.ValueOf(params[1]).Kind() {
	case reflect.String:
		fileList = []string{params[1].(string)}
	case reflect.Slice:
		fileList = params[1].([]string)
	default:
		return false, "file list must be slice of strings"
	}
	sort.Strings(fileList)

	dir, err := os.Open(dirPath)
	if err != nil {
		return false, err.Error()
	}
	defer dir.Close()
	fileinfos, err := dir.Readdir(-1)
	if err != nil {
		return false, err.Error()
	}

	actualFilenames := make([]string, 0)
	for _, fi := range fileinfos {
		actualFilenames = append(actualFilenames, fi.Name())
	}
	sort.Strings(actualFilenames)

	return DeepEquals.Check([]interface{}{actualFilenames, fileList}, []string{"obtained", "expected"})
}
