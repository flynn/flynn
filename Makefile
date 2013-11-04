VERSION = 0.1.0

build: sdutil

sdutil:
	go build -o sdutil
	GOOS=linux go build -o sdutil.linux

package: build
	rm -rf package
	mkdir -p package/bin
	mv sdutil.linux package/bin/sdutil
	cd package && fpm -s dir -t deb -n sdutil -v ${VERSION} --prefix /usr/local bin

release: package
	s3cmd -P put package/sdutil_${VERSION}_amd64.deb s3://progrium-sdutil

clean:
	rm -rf sdutil sdutil.linux package
