build/container: build/flynn-host Dockerfile manifest.json
	docker build -t flynn/host .
	touch build/container

build/flynn-host: Godeps *.go sampi/*.go types/*.go
	godep go build -o build/flynn-host

.PHONY: clean
clean:
	rm -rf build
