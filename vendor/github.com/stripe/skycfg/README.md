# Skycfg

Skycfg is an extension library for the [Starlark language](https://github.com/bazelbuild/starlark) that adds support for constructing [Protocol Buffer](https://developers.google.com/protocol-buffers/) messages. It was developed by Stripe to simplify configuration of Kubernetes services, Envoy routes, Terraform resources, and other complex configuration data.

At present, only the Go implementation of Starlark is supported.

[![API Documentation](https://godoc.org/github.com/stripe/skycfg?status.svg)](https://godoc.org/github.com/stripe/skycfg)
[![Build Status](https://travis-ci.org/stripe/skycfg.svg?branch=master)](https://travis-ci.org/stripe/skycfg)
[![Test Coverage](https://coveralls.io/repos/github/stripe/skycfg/badge.svg?branch=master)](https://coveralls.io/github/stripe/skycfg?branch=master)

## Getting Started

The entry point to Skycfg is the [`skycfg.Load()`](https://godoc.org/pkg/github.com/stripe/skycfg/#Load) function, which reads a configuration file from local disk. As the implementation stabilizes we expect to expand the public API surface so that Skycfg can be combined with other Starlark extensions.

Lets start with a simple `main` function that prints out every Protobuf message created by the config file `hello.sky`:

```go
package main

import (
    "context"
    "fmt"
    "github.com/stripe/skycfg"
    _ "github.com/golang/protobuf/ptypes/wrappers"
)

func main() {
    ctx := context.Background()
    config, err := skycfg.Load(ctx, "hello.sky")
    if err != nil { panic(err) }
    messages, err := config.Main(ctx)
    if err != nil { panic(err) }
    for _, msg := range messages {
        fmt.Printf("%s\n", msg.String())
    }
}
```

```python
# hello.sky
pb = proto.package("google.protobuf")

def main(ctx):
  return [pb.StringValue(value = "Hello, world!")]
```

Now we can build a small test driver and see what values are returned:

```
$ go get github.com/stripe/skycfg
$ go build -o test-skycfg
$ ./test-skycfg
value:"Hello, world!"
$
```

Success!

For more in-depth examples covering specific topics, see the `_examples/` directory:
* [`_examples/repl`](https://github.com/stripe/skycfg/tree/master/_examples/repl): Interactive evaluation of a Skycfg file
* [`_examples/k8s`](https://github.com/stripe/skycfg/tree/master/_examples/k8s): Basic Kubernetes integration
* [`_examples/wasm`](https://github.com/stripe/skycfg/tree/master/_examples/wasm): Skycfg in the browser with WebAssembly

## Why use Skycfg?

Compared to bare YAML or TOML, the Python-like syntax of Skycfg might not seem like a win. Why would we want configuration files with all those quotes and colons and braces?

There are four important benefits to using Skycfg over YAML:

### Type Safety

Protobuf has a statically-typed data model, which means the type of every field is known to Skycfg when it's building your configuration. There is no risk of accidentally assigning a string to a number, a struct to a different struct, or forgetting to quote a YAML value.

```python
pb = proto.package("google.protobuf")

def main(ctx):
  return [pb.StringValue(value = 123)]
```

```
$ ./test-skycfg
panic: TypeError: value 123 (type `int') can't be assigned to type `string'.
```

### Functions

As in standard Python, you can define helper functions to reduce duplicated typing and share logic.

```python
pb = proto.package("google.protobuf")

def greet(lang):
  greeting = {
    "en": "Hello, world!",
    "fr": "Bonjour, monde!",
  }[lang]
  return pb.StringValue(value = greeting)

def main(ctx):
  return [greet("en"), greet("fr")]
```
```
$ ./test-skycfg
value:"Hello world!"
value:"Bonjour, monde!"
```

### Modules

Starlark supports importing modules from other files, which you can use to share common code between configurations. By default the paths to load are resolved on the local filesystem, but you can also override the `load()` handler to support syntaxes such as [Bazel-style target labels](https://docs.bazel.build/versions/master/build-ref.html).

Modules can protect service owners from complex Kubernetes logic:

```python
load("//config/common/kubernetes.sky", "kubernetes")

def my_service(ctx):
  return kubernetes.pod(
      name = "my-namespace/my-service",
      containers = [
          kubernetes.container(name = "main", image = "index.docker.io/hello-world"),
      ]
  )
```

When combined with VCS hooks like [GitHub CODEOWNERS](https://help.github.com/articles/about-codeowners/), you can use modules to provide an API surface for third-party tools deployed in your infrastructure:

```python
load("//config/common/constants.sky",  "CLUSTERS")
load("//config/production/k8s_dashboard/v1.10.0/main.sky",
     "kubernetes_dashboard")

def main(ctx):
  return [
    kubernetes_dashboard(ctx, cluster = CLUSTERS['ord1']),
    kubernetes_dashboard(ctx, cluster = CLUSTERS['ord1-canary']),
  ]
```

### Context Variables

Skycfg supports limited dynamic behavior through the use of _context variables_, which let the Go caller pass arbitrary key:value pairs in the `ctx` parameter.

```go
func main() {
    // ...
    messages, err := config.Main(ctx, skycfg.WithVars(starlark.StringDict{
      "revision": starlark.String("master/12345"),
    }))
```

```python
pb = proto.package("google.protobuf")

def main(ctx):
  print("ctx.vars:", ctx.vars)
  return []
```

```
$ ./test-skycfg
[hello.sky:4] ctx.vars: {"revision": "master/12345"}
```

## Contributing

We welcome contributions from the community. For small simple changes, go ahead and [open a pull request](https://github.com/stripe/skycfg/compare). Larger changes should start out in the issue tracker, so we can make sure they fit into the roadmap. Changes to the Starlark language itself (such as new primitive types or syntax) should be applied to https://github.com/google/starlark-go.

## Stability

Skycfg depends on internal details of the go-protobuf generated code, and as such may need to be updated to work with future versions of go-protobuf. We will release Skycfg v1.0 after all dependencies on go-protobuf implementation details have been fixed, which will be after the "api-v2" branch lands in a stable release of go-protobuf.

Our existing public APIs are expected to be stable even before the v1.0 release. Symbols that will change before v1.0 are hidden from the public docs and named `Unstable*`.

## Known issues

### TypeError with the same type names

E.g.:
```
panic: TypeError: value <google.protobuf.UInt32Value value:20 > (type `google.protobuf.UInt32Value') can't be assigned to type `google.protobuf.UInt32Value'.
```

On the face of it this looks confusing, and it is. It's an effect of a
project that use `gogo_protobuf`, where some fundamental types are
wrapped in messages that shadow the same messages defined by
`google.protobuf`. Even though the messages seem to say that they are
of the same type, they are actually two different types with the same
name.

For projects that use `gogo_protobuf`, e.g. envoy, this can be very confusing.

To resolve this, use the package `github.com/stripe/skycfg/gogocopmat`
in your go code. This will allow skycfg code to load
`proto.package("gogo:google.protobuf") and the internal registry it
creates will handle the conversion of these types.
