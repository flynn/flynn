FROM busybox
MAINTAINER Jeff Lindsay <progrium@gmail.com>

ADD ./shelf /bin/shelf
ENTRYPOINT ["/bin/shelf"]