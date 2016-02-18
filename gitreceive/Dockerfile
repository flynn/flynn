FROM ubuntu:trusty-20160217

RUN apt-get update && apt-get -qy install git && apt-get clean
ADD start.sh /bin/start-flynn-receiver
ADD flynn-receiver /bin/flynn-receiver
ADD gitreceived /bin/gitreceived

CMD ["/bin/start-flynn-receiver"]
