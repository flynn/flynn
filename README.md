# flynn-api

This is the Flynn HTTP API server. It is loosely based on the [Heroku Platform
API](https://devcenter.heroku.com/articles/platform-api-reference) and enables
management of applications running on Flynn.

Currently the only consumer of the API is
[flynn-cli](https://github.com/flynn/flynn-cli).

flynn-api communicates with [sampi](https://github.com/flynn/sampi) and
[lorne](https://github.com/flynn/lorne) to get the current state and run jobs.
