build/container: build/flynn-controller
	docker build -t flynn/controller .
	touch build/container

build/flynn-controller:
	godep go build -o build/flynn-controller

.PHONY: clean
clean:
	rm -rf build
