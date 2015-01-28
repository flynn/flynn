all:
	docker build -t tuf-gen ./generate
	docker run tuf-gen | tar x

clean:
	rm -rf with{,out}-consistent-snapshot

.PHONY: all clean
