FROM flynn/busybox

ADD ./bin/discoverd /bin/discoverd
ADD start.sh /bin/start-discoverd

ENTRYPOINT ["/bin/start-discoverd"]
