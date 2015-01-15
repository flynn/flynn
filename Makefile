GIT_COMMIT=`git rev-parse --short HEAD`
GIT_BRANCH=`git rev-parse --abbrev-ref HEAD`
GIT_TAG=`git describe --tags --exact-match --match "v*" 2>/dev/null || echo "none"`
GIT_DIRTY=`test -n "$(git status --porcelain)" && echo true || echo false`

all:
	@GIT_COMMIT=$(GIT_COMMIT) GIT_BRANCH=$(GIT_BRANCH) GIT_TAG=$(GIT_TAG) GIT_DIRTY=$(GIT_DIRTY) tup

dev:
	@GIT_COMMIT=dev GIT_BRANCH=dev GIT_TAG=none GIT_DIRTY=false tup

clean:
	git clean -Xdf -e '!.tup' -e '!.vagrant' -e '!script/custom-vagrant'

.PHONY: all clean dev
