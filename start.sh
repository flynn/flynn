#!/bin/sh

exec /bin/sdutil exec -s gitreceive:$PORT /bin/gitreceived /bin/flynn-key-check /bin/flynn-receive
