build/container: build/flynn-controller build/flynn-scheduler Dockerfile start.sh
	docker build -t flynn/controller .
	touch build/container

build/flynn-controller: Godeps *.go types/*.go utils/*.go
	godep go build -o build/flynn-controller

build/flynn-scheduler: Godeps scheduler/*.go client/*.go types/*.go utils/*.go
	godep go build -o build/flynn-scheduler ./scheduler

.PHONY: clean
clean:
	rm -rf build
