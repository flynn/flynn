# Sampi ϡ

Sampi is the Flynn scheduling framework. It keeps the state of the cluster in
memory and serializes job transactions from schedulers. After a batch of jobs is
successfully committed, Sampi sends the jobs to the relevant [host
service](https://github.com/flynn/lorne) instances to be run.

Sampi is inspired by [Google
Omega](http://eurosys2013.tudos.org/wp-content/uploads/2013/paper/Schwarzkopf.pdf).

## TODO

- Use discovery heartbeat from lorne to signal downtime
- Introspection tool
- Standardize container config (remove Docker-specific structures)
- Add more tests
