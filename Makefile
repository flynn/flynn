build: build/container build/discoverd

build/container: build/discoverd
	docker build -t flynn/discoverd .
	touch build/container

build/discoverd: Godeps *.go agent/*.go
	godep go build -o build/discoverd

test:
	godep go test ./...

.PHONY: clean test
clean:
	rm -rf build
