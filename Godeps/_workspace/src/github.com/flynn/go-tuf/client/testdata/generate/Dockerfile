FROM ubuntu:trusty

RUN apt-get update
RUN apt-get install -y python python-dev python-pip libffi-dev tree

# Use the develop branch of tuf for the following fix:
# https://github.com/theupdateframework/tuf/commit/38005fe
RUN apt-get install -y git
RUN pip install --no-use-wheel git+https://github.com/theupdateframework/tuf.git@develop && pip install tuf[tools]

ADD generate.py generate.sh /
CMD /generate.sh
