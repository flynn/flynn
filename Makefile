bin/container: bin/flynn-mongodb Dockerfile *.sh
	docker build -t flynn/mongodb .
	touch bin/container

bin/flynn-mongodb: Godeps *.go
	godep go build -o bin/flynn-mongodb

.PHONY: clean
clean:
	rm -rf bin
