build/container: build/flynn-bootstrap Dockerfile bootstrapper/manifest.json
	docker build -t flynn/bootstrap .
	touch build/container

build/flynn-bootstrap: Godeps *.go bootstrapper/*.go
	godep go build -o build/flynn-bootstrap ./bootstrapper

.PHONY: clean
clean:
	rm -rf build
