FROM flynn/cedarish

ADD ./builder/ /tmp/builder
# Explicitly number the buildpacks directory based on the order of buildpacks.txt
RUN nl -nrz /tmp/builder/buildpacks.txt | awk '{print $2 "\t" $1}' | xargs -L 1 /tmp/builder/install-buildpack /tmp/buildpacks && \
    chown -R nobody:nogroup /tmp/buildpacks
ENTRYPOINT ["/tmp/builder/build.sh"]
CMD []
