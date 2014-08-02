# Grohl

Grohl is an opinionated library for gathering metrics and data about how your
applications are running in production.  It does this through writing logs
in a key=value structure.  It also provides interfaces for sending exceptions
or metrics to external services.

This is a Go version of [asenchi/scrolls](https://github.com/asenchi/scrolls).
The name for this library came from mashing the words "go" and "scrolls"
together.  Also, Dave Grohl (lead singer of Foo Fighters) is passionate about
event driven metrics.

See this [blog post][blog] for the rationale behind this library.

[blog]: http://techno-weenie.net/2013/11/2/key-value-logs-in-go/

## Installation

    $ go get github.com/technoweenie/grohl

Then import it:

    import "github.com/technoweenie/grohl"

## Usage

Grohl takes almost no setup.  Everything writes to STDOUT by default.  Here's a
quick http server example:

```go
package main

import (
  "github.com/technoweenie/grohl"
  "log"
  "net/http"
)

func main() {
  grohl.AddContext("app", "example")

  http.HandleFunc("/foo", func(w http.ResponseWriter, r *http.Request) {
    grohl.Log(grohl.Data{"path": r.URL.Path})
    fmt.Fprintf(w, "Hello, %q", html.EscapeString(r.URL.Path))
  })

  log.Fatal(http.ListenAndServe(":8080", nil))
}
```

This writes a log on every HTTP request like:

    now=2013-10-14T15:04:05-0700 app=example path=/foo

See the [godocs](http://godoc.org/github.com/technoweenie/grohl) for details on
metrics, statsd integration, and custom error reporters.

## Note on Patches/Pull Requests

1. Fork the project on GitHub.
2. Make your feature addition or bug fix.
3. Add tests for it. This is important so I don't break it in a future version
   unintentionally.
4. Commit, do not mess with rakefile, version, or history. (if you want to have
   your own version, that is fine but bump version in a commit by itself I can
   ignore when I pull)
5. Send me a pull request. Bonus points for topic branches.
