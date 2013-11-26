FROM busybox
MAINTAINER Jeff Lindsay <progrium@gmail.com>

ADD ./build/shelf /bin/shelf
ADD ./build/sdutil /bin/sdutil

EXPOSE 8080
ENTRYPOINT ["/bin/shelf"]