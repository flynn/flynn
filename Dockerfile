FROM progrium/cedarish
MAINTAINER progrium "progrium@gmail.com"

ADD ./runner/ /runner
ENTRYPOINT ["/runner/init"]