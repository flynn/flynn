FROM flynn/busybox

ADD ./bin/discoverd /bin/discoverd
ADD ./bin/discoverd-wrapper /bin/discoverd-wrapper

ENTRYPOINT ["/bin/discoverd-wrapper"]
