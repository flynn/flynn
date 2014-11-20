#!/bin/sh

exec /bin/sdutil exec -s dashboard-web:$PORT /bin/flynn-dashboard
