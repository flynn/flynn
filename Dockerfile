FROM flynn/busybox
MAINTAINER Jonathan Rudenberg <jonathan@titanous.com>

ADD ./build/shelf /bin/shelf
ADD ./build/sdutil /bin/sdutil

EXPOSE 8080
ENTRYPOINT ["/bin/shelf"]
