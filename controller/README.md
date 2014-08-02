# flynn-controller

This is the Flynn Controller. It is inspired by the [Heroku Platform
API](https://devcenter.heroku.com/articles/platform-api-reference) and enables
management of applications running on Flynn via an HTTP API.

The controller depends on PostgreSQL and is typically booted by
[flynn-bootstrap](https://github.com/flynn/flynn-bootstrap).

The API is in a state of flux and is undocumented.
[flynn-cli](https://github.com/flynn/flynn-cli) is one of the API consumers.
