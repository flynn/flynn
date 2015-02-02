FROM ubuntu-debootstrap:14.04

RUN apt-get update && apt-get -qy install git && apt-get clean
ADD start.sh /bin/start-flynn-receiver
ADD flynn-receiver /bin/flynn-receiver
ADD bin/flynn-key-check /bin/flynn-key-check
ADD bin/gitreceived /bin/gitreceived

CMD ["/bin/start-flynn-receiver"]
