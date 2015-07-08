package main

// hack in imports to make godep happy about some binaries we vendor
import (
	_ "github.com/flynn/flynn/Godeps/_workspace/src/github.com/jteeuwen/go-bindata"
)

func main() {}
