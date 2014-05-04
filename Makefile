build: build/container build/discoverd

build/container: build/discoverd
	docker build -t flynn/discoverd .
	touch build/container

build/discoverd: Godeps *.go agent/*.go
	godep go build -o build/discoverd

.PHONY: clean
clean:
	rm -rf build
