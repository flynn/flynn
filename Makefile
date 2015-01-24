GIT_COMMIT=`git rev-parse --short HEAD`
GIT_BRANCH=`git rev-parse --abbrev-ref HEAD`
GIT_TAG=`git describe --tags --exact-match --match "v*" 2>/dev/null || echo "none"`
GIT_DIRTY=`test -n "$(git status --porcelain)" && echo true || echo false`

all:
	@GIT_COMMIT=dev GIT_BRANCH=dev GIT_TAG=none GIT_DIRTY=false tup

dev:
	@echo 'dev is no longer a valid target, just run `make`'

release:
	@GIT_COMMIT=$(GIT_COMMIT) GIT_BRANCH=$(GIT_BRANCH) GIT_TAG=$(GIT_TAG) GIT_DIRTY=$(GIT_DIRTY) tup

clean:
	git clean -Xdf -e '!.tup' -e '!.vagrant' -e '!script/custom-vagrant'

test: test-unit test-integration

test-unit:
	@GIT_COMMIT=dev GIT_BRANCH=dev GIT_TAG=none GIT_DIRTY=false tup appliance/etcd discoverd
	go test ./...

test-integration:
	script/run-integration-tests

.PHONY: all clean dev release test test-unit test-integration
