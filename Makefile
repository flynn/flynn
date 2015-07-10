GIT_COMMIT=`git rev-parse --short HEAD`
GIT_BRANCH=`git rev-parse --abbrev-ref HEAD`
# NOTE: the `git tag` command is filtered through `grep .` so it returns non-zero when empty
GIT_TAG=`git tag --list "v*" --sort "v:refname" --points-at HEAD 2>/dev/null | tail -n 1 | grep . || echo "none"`
GIT_DIRTY=`test -n "$(git status --porcelain)" && echo true || echo false`

all: toolchain
	@GIT_COMMIT=dev GIT_BRANCH=dev GIT_TAG=none GIT_DIRTY=false tup

release: toolchain
	@GIT_COMMIT=$(GIT_COMMIT) GIT_BRANCH=$(GIT_BRANCH) GIT_TAG=$(GIT_TAG) GIT_DIRTY=$(GIT_DIRTY) tup

clean:
	git clean -Xdf -e '!.tup' -e '!.vagrant' -e '!script/custom-vagrant'

test: test-unit test-integration

test-unit:
	@GIT_COMMIT=dev GIT_BRANCH=dev GIT_TAG=none GIT_DIRTY=false tup appliance/etcd discoverd
	@GOROOT=`readlink -f util/_toolchain/go` util/_toolchain/go/bin/go test ./...

test-integration:
	script/run-integration-tests

toolchain:
	@cd util/_toolchain && ./build.sh

.PHONY: all clean dev release test test-unit test-integration
