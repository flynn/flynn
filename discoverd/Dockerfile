FROM flynn/busybox:trusty-20160217

ADD ./bin/discoverd /bin/discoverd
ADD start.sh /bin/start-discoverd

ENTRYPOINT ["/bin/start-discoverd"]
