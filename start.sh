#!/bin/sh

echo "$SSH_PRIVATE_KEY" > /tmp/id_rsa
/bin/sdutil exec -s gitreceive:$PORT /bin/gitreceived /tmp/id_rsa /bin/flynn-key-check /bin/flynn-receive
