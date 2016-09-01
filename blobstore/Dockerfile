FROM ubuntu:trusty-20160217

RUN apt-get update && apt-get -qy install ca-certificates && apt-get clean
ADD ./bin/flynn-blobstore /bin/flynn-blobstore
ADD ./bin/flynn-blobstore-migrate /bin/flynn-blobstore-migrate
ADD ./bin/flynn-blobstore-cleanup /bin/flynn-blobstore-cleanup

ENTRYPOINT ["/bin/flynn-blobstore"]
