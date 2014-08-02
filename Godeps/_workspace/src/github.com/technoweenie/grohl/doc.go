/*
Grohl is an opinionated library for gathering metrics and data about how your
applications are running in production.  It does this through writing logs
in a key=value structure.  It also provides interfaces for sending stacktraces
or metrics to external services.

This is a Go version of https://github.com/asenchi/scrolls. The name for this
library came from mashing the words "go" and "scrolls" together.  Also, Dave 
Grohl (lead singer of Foo Fighters) is passionate about event driven metrics.

Grohl treats logs as the central authority for how an application is behaving.
Logs are written in a key=value structure so that they are easily parsed.  If
you use a set of common log keys, you can relate events from various services
together.

Here's an example log that you might write:

  grohl.Log(grohl.Data{"fn": "trap", "signal": "TERM", "at": "exit", "status": 0})

The output would look something like:

  now=2013-10-14T15:04:05-0700 fn=trap signal=TERM at=exit status=0

Note: Other examples leave out the "now" keyword for clarity.

A *grohl.Context stores a map of keys and values that are written with every
log message.  You can set common keys for every request, or create a new context
per new request or connection.

You can add more context to the example above by setting up the app name and
deployed environment.

  grohl.AddContext("app", "myapp")
  grohl.AddContext("deploy", os.Getenv("DEPLOY"))

This changes the output from above to:

  app=myapp deploy=production fn=trap signal=TERM at=exit status=0

You can also create scoped Context objects.  For instance, a network server may
want a scoped Context for each request or connection.

  context := grohl.NewContext(grohl.Data{"ns": "server"})
  context.Log(grohl.Data{"fn": "trap", "signal": "TERM", "at": "exit", "status": 0})

This is the output (taking the global context above into consideration):

  app=myapp deploy=production ns=server fn=trap signal=TERM at=exit status=0

As you can see we have some standard nomenclature around logging. Here's a cheat sheet for some of the methods we use:

  * now: The current timestamp, automatically set by an IoLogger.  Can be disabled
  if IoLogger.AddTime is disabled.
  * app: Application
  * lib: Library
  * ns: Namespace (Class, Module or files)
  * fn: Function
  * at: Execution point
  * deploy: Our deployment (typically an environment variable i.e. DEPLOY=staging)
  * elapsed: Measurements (Time from a Timer)
  * metric: The name of a Statter measurement
  * count: Measurements (Counters through a Statter)
  * gauge: Measurements (Gauges through a Statter)
  * timing: Measurements (Timing through a Statter)

By default, all *grohl.Context objects write to STDOUT.  Grohl includes support
for both io and channel loggers.

  writer, _ := syslog.Dial(network, raddr, syslog.LOG_INFO, tag)
  grohl.SetLogger(grohl.NewIoLogger(writer))

If you are writing to *grohl.Context objects in separate go routines, a
channel logger can be used for concurrency.

  // you can pass in your own "chan grohl.data" too.
  chlogger, ch := grohl.NewChannelLogger(nil)
  grohl.SetLogger(chlogger)

  // pipe messages from the channel to a single io.writer:
  writer, _ := syslog.Dial(network, raddr, syslog.LOG_INFO, tag)
  logger := grohl.NewIoLogger(writer)

  // reads from the channel until the program dies
  go grohl.Watch(logger, ch)

Grohl provides a grohl.Statter interface based on https://github.com/peterbourgon/g2s:

  // these functions are available on a *grohl.Context too
  grohl.Counter(1.0, "my.silly.counter", 1)
  grohl.Timing(1.0, "my.silly.slow-process", time.Since(somethingBegan))
  grohl.Gauge(1.0, "my.silly.status", "green")

Without any setup, this outputs:

  metric=my.silly.counter count=1
  metric=my.silly.slow-process timing=12345
  metric=my.silly.status gauge=green

If you import "github.com/peterbourgon/g2s", you can dial into a statsd server
over a udp socket:

  statter, err := g2s.Dial("udp", "statsd.server:1234")
  if err != nil {
    panic(err)
  }
  grohl.CurrentStatter = statter

Once being set up, the statter functions above will not output any logs.

Grohl makes it easy to measure the run time of a portion of code.

  // you can also create a timer from a *grohl.Context
  // timer := context.Timer(grohl.Data{"fn": "test"})
  timer := grohl.NewTimer(grohl.Data{"fn": "test"})
  grohl.Log(grohl.Data{"status": "exec"})
  timer.Finish()

This would output:

  fn=test at=start
  status=exec
  fn=test at=finish elapsed=0.300

You can change the time unit that Grohl uses to "milliseconds" (the default is
"seconds"):

  grohl.SetTimeUnit("ms")

  // or with a *grohl.Context
  context.TimeUnit = "ms"

You can also write to a custom Statter:

  timer := grohl.NewTimer(grohl.data{"fn": "test"})
  // uses grohl.CurrentStatter by default
  timer.SetStatter(nil, 1.0, "my.silly.slow-process")
  timer.Finish()

You can also set all *grohl.Timer objects to use the same statter.

  // You can call SetStatter() on a *grohl.Context to affect any *grohl.Timer
  // objects created from it.
  //
  // This affects _all_ *grohl.Timer objects.
  grohl.SetStatter(nil, 1.0, "my.silly")

  timer := grohl.NewTimer(grohl.data{"fn": "test"})

  // set just the suffix of the statter bucket set above
  timer.StatterBucketSuffix("slow-process")

  // overwrite the whole bucket
  timer.StatterBucket = "my.silly.slow-process"

  // Sample only 50% of the timings.
  timer.StatterSampleRate = 0.5

  timer.Finish()

Grohl can report Go errors:

  written, err := writer.Write(someBytes)
  if err ! nil {
    // context has the following from above:
    // grohl.Data{"app": "myapp", "deploy": "production", "ns": "server"}
    context.Report(err, grohl.Data{"written": written})
  }

Without any ErrorReporter set, this logs the following:

  app=myapp deploy=production ns=server at=exception class=*errors.errorString message="some message"
  app=myapp deploy=production ns=server at=exception class=*errors.errorString message="some message" site="stack trace line 1"
  app=myapp deploy=production ns=server at=exception class=*errors.errorString message="some message" site="stack trace line 2"
  app=myapp deploy=production ns=server at=exception class=*errors.errorString message="some message" site="stack trace line 3"

You can set the default ErrorReporter too:

  myReporter := myreporter.New()
  grohl.SetErrorReporter(myReporter)

*/
package grohl
