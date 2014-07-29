flynn-test: flynn-test-runner *.go
	godep go build -o flynn-test

flynn-test-runner: Godeps runner/*.go arg/*.go cluster/*.go util/*.go
	godep go build -o flynn-test-runner ./runner

clean:
	rm flynn-test flynn-test-runner
