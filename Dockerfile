FROM progrium/cedarish
MAINTAINER Jonathan Rudenberg <jonathan@titanous.com>

ADD ./runner/ /runner
ADD ./build/sdutil /bin/sdutil
ENTRYPOINT ["/runner/init"]
