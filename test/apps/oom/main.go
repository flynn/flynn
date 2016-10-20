package main

import "bytes"

func main() {
	var buf bytes.Buffer
	for {
		buf.Write(make([]byte, 1024*1024))
	}
}
