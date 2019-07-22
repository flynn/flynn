package main

import (
	"io/ioutil"
	"os"
)

// This file is replaced with a version with all assets compiled into it via
// the flynn build process.

func Asset(path string) ([]byte, error) {
	return ioutil.ReadFile(path)
}

func AssetInfo(path string) (os.FileInfo, error) {
	return os.Stat(path)
}
