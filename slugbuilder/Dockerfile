FROM flynn/cedarish

ADD builder/buildpacks.txt /builder/
ADD builder/install-buildpack /builder/
# Explicitly number the buildpacks directory based on the order of buildpacks.txt
RUN nl -nrz /builder/buildpacks.txt | awk '{print $2 "\t" $1}' | xargs -L 1 /builder/install-buildpack /builder/buildpacks

RUN apt-get install --yes squashfs-tools

ADD convert-legacy-slug.sh /bin/convert-legacy-slug.sh
ADD bin/create-artifact /bin/create-artifact
ADD builder/build.sh /builder/
ADD builder/create-user.sh /builder/

ENTRYPOINT ["/builder/build.sh"]
CMD []
