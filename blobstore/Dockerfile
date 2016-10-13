FROM ubuntu:trusty-20160217

RUN apt-get update && apt-get -qy install ca-certificates curl && apt-get clean
ADD ./bin/flynn-blobstore /bin/flynn-blobstore

ENTRYPOINT ["/bin/flynn-blobstore", "server"]
