
build:
	cd gitreceived && go build

install: build
	cp gitreceived/gitreceived /usr/local/bin