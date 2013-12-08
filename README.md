# Lorne

Lorne is the Flynn host service. An instance of Lorne runs on every host in the
Flynn cluster. Lorne is responsible for running jobs (Docker containers) and
reporting back to schedulers and the [scheduling
framework](https://github.com/flynn/sampi).

## TODO

- Recover from crashes/restarts
- Increase test coverate
- Documentation
- Robust port allocation
- Support for job state (Docker volumes)
