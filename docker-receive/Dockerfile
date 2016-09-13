FROM ubuntu:trusty-20160217

RUN apt-get install --yes squashfs-tools

ADD bin/docker-receive /bin/docker-receive
ADD bin/docker-artifact /bin/docker-artifact
ADD bin/docker-migrator /bin/docker-migrator
ADD bin/ca-certs.pem /etc/ssl/certs/ca-certs.pem

CMD ["/bin/docker-receive"]
