FROM progrium/cedarish
MAINTAINER progrium "progrium@gmail.com"

RUN useradd slugbuilder --home-dir /app

# allow user RW access to /app
RUN mkdir /app
RUN chown -R slugbuilder:slugbuilder /app

ADD ./builder/ /tmp/builder
RUN mkdir -p /tmp/buildpacks && cd /tmp/buildpacks && xargs -L 1 git clone --depth=1 < /tmp/builder/buildpacks.txt
RUN chown -R slugbuilder:slugbuilder /tmp/buildpacks
ENTRYPOINT ["/tmp/builder/build.sh"]
USER slugbuilder
