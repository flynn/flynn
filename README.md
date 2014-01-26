# flynn-host

flynn-host is the Flynn host service. An instance of it runs on every host in
the Flynn cluster. It is responsible for running jobs (Docker containers) and
reporting back to schedulers and the leader.

## TODO

- Recover from crashes/restarts
- Increase test coverage
- Documentation
- Robust port allocation
- Support for job state (Docker volumes)
