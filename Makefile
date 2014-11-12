all:
	@tup

clean:
	git clean -Xdf -e '!.tup' -e '!.vagrant'

.PHONY: all clean
