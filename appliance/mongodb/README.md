flynn-mongodb
==============

Flynn MongoDB database appliance.

This appliance is automatically managed by Flynn. To run it standalone, use
a `docker run` command like this:

    docker run
       -v /srv/data:/data
       -e PORT=5555
       -e EXTERNAL_IP=10.0.2.15
       -e DISCOVERD=10.0.2.15:1111
       -p 5555:5555 flynn/mongodb mongo
