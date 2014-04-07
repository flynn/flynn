bin/container: bin/flynn-postgres bin/flynn-postgres-api Dockerfile *.sh *.conf
	docker build -t flynn/postgres .
	touch bin/container

bin/flynn-postgres: Godeps *.go
	godep go build -o bin/flynn-postgres

bin/flynn-postgres-api: api/Godeps api/*.go
	cd api && godep go build -o ../bin/flynn-postgres-api

.PHONY: clean
clean:
	rm -rf bin
