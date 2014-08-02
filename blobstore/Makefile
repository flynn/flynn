build/container: build/shelf Dockerfile
	docker build -t flynn/shelf .
	touch build/container

build/shelf: Godeps *.go
	godep go build -o build/shelf

.PHONY: clean
clean:
	rm -rf build
