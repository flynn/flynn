FROM ubuntu:trusty-20160217

RUN apt-get update && \
    apt-get -qy install git curl squashfs-tools && \
    apt-get clean

RUN curl -fsSLo "/usr/local/bin/docker" "https://get.docker.com/builds/Linux/x86_64/docker-1.9.1" && \
    chmod +x "/usr/local/bin/docker"

RUN git config --global "user.email" "test@flynn.io" &&\
    git config --global "user.name"  "Flynn Test"

ADD bin/flynn-test /bin/flynn-test
ADD bin/flynn-test-file-server /bin/flynn-test-file-server
ADD run.sh /bin/run-flynn-test.sh

CMD ["/bin/run-flynn-test.sh"]
