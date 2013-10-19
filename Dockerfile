FROM progrium/cedarish
MAINTAINER progrium "progrium@gmail.com"

ADD ./builder/ /tmp/builder
RUN mkdir -p /tmp/buildpacks && cd /tmp/buildpacks && xargs -L 1 git clone --depth=1 < /tmp/builder/buildpacks.txt
ENTRYPOINT ["/tmp/builder/build.sh"]