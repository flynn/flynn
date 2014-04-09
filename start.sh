#!/bin/sh

echo "$SSH_PRIVATE_KEY" > /tmp/id_rsa
/bin/gitreceived /tmp/id_rsa true /bin/flynn-receive
