FROM flynn/cedarish

ADD builder/buildpacks.txt /tmp/builder/
ADD builder/install-buildpack /tmp/builder/
# Explicitly number the buildpacks directory based on the order of buildpacks.txt
RUN nl -nrz /tmp/builder/buildpacks.txt | awk '{print $2 "\t" $1}' | xargs -L 1 /tmp/builder/install-buildpack /tmp/buildpacks && \
    chown -R nobody:nogroup /tmp/buildpacks

RUN apt-get install --yes squashfs-tools

ADD bin/create-artifact /bin/create-artifact
ADD builder/build.sh /tmp/builder/

ENTRYPOINT ["/tmp/builder/build.sh"]
CMD []
