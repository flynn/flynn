# sdutil

Service discovery utility for systems based on [go-discover](/discoverd/client).

Complete work in progress

```
sdutil register -a foo=bar www:$PORT
sdutil services -1 -a www
sdutil exec -s www:$PORT /path/to/www/daemon $PORT
sdutil check
```
