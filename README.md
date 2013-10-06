# Lorne

Lorne is the Flynn host service. An instance of Lorne runs on every host in the
Flynn cluster. Lorne is responsible for running jobs (Docker containers) and
reporting back to schedulers and the [scheduling
framework](https://github.com/flynn/sampi).

## TODO

- Track resources
- RPC interface for schedulers
- Recover from crashes
- Use service discovery to find/follow scheduler
- Add containers to service discovery
- PTY rendezvous
- Log shipping
- Write tests
