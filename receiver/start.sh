#!/bin/sh

exec /bin/gitreceived --auth-checker /bin/flynn-key-check --receiver /bin/flynn-receiver --cache-key-hook /bin/flynn-cache-key
