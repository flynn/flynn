FROM progrium/cedarish
MAINTAINER progrium "progrium@gmail.com"

ADD ./runner/ /runner
RUN cd /tmp && wget http://progrium-sdutil.s3.amazonaws.com/sdutil_0.1.0_amd64.deb && dpkg -i *.deb
ENTRYPOINT ["/runner/init"]