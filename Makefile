build/container: build/strowger Dockerfile
	docker build -t flynn/strowger .
	touch build/container

build/strowger: *.go types/*.go
	godep go build -o build/strowger

.PHONY: clean
clean:
	rm -rf build
