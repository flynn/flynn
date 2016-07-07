FROM flynn/busybox:trusty-20160217

ADD bin/docker-receive /bin/docker-receive
ADD bin/ca-certs.pem /etc/ssl/certs/ca-certs.pem

CMD ["/bin/docker-receive"]
