# Sampi ϡ

Sampi is the Flynn scheduling framework. It keeps the state of the cluster in
memory and serializes job transactions from schedulers. After a batch of jobs is
successfully committed, Sampi sends the jobs to the relevant [host
service](https://github.com/flynn/lorne) instances to be run.

## TODO

- Use [service discovery](https://github.com/flynn/go-discover) for leader
  election and standby mode
- Implement robust host failure logic
- Introspection tool
- Abstract docker config out into another service?
- Add more tests
