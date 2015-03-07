#!/bin/sh

exec /bin/gitreceived --auth-checker /bin/flynn-key-check --receiver /bin/flynn-receiver
