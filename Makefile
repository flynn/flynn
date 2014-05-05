build: build/container build/discoverd

build/container: build/discoverd
	docker build -t flynn/discoverd .
	touch build/container

build/discoverd: Godeps *.go agent/*.go
	godep go build -o build/discoverd

lint:
	go get github.com/golang/lint
	golint *.go **/*.go

fmt:
	gofmt -w -s .

test:
	godep go test ./...

.PHONY: clean test lint fmt
clean:
	rm -rf build
