# Discover for Go (alpha)

A simple but powerful service discovery system written in Go. It's currently backed by etcd, but can be
extended to use ZooKeeper or other distributed consistent stores. The client is lightweight enough to 
also be implemented in other languages.

## Todo

 * Attributes are only half implemented, but design is in place
 * Better error propagation
 * More test coverage
 * More documentation