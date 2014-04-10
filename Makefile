build: Dockerfile builder/*
	docker build -t flynn/slugbuilder .
	touch build

.PHONY: clean
clean:
	rm -rf build
