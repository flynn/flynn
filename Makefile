
build:
	cd gitreceived && go get && go build

install: build
	cp gitreceived/gitreceived /usr/local/bin