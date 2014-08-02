package grohl

import "time"

// Data is the map used to specify the key/value pairs for a logged message.
type Data map[string]interface{}

// The Logger interface represents the ability to log key/value data.
type Logger interface {
	Log(Data) error
}

// CurrentLogger is the default Logger used by Log, Report.
var CurrentLogger Logger = NewIoLogger(nil)

// CurrentContext is the default Context used by Log, Report, AddContext,
// DeleteContext, NewTimer.
var CurrentContext = newContext(make(Data), CurrentLogger, "s", nil, &_statter{})

// The CurrentStatter is the default Statter used in Counter, Timing, Gauge.
var CurrentStatter Statter = CurrentContext

// Log writes the key/value data to the CurrentLogger.
func Log(data Data) {
	CurrentContext.Log(data)
}

// Report sends the error and key/value data to the CurrentContext's
// ErrorReporter.  If no reporter is set, the CurrentContext simply logs the
// error and stacktrace.
func Report(err error, data Data) {
	CurrentContext.Report(err, data)
}

// Counter writes a counter value to the CurrentStatter.  By default, values are
// simply logged.
func Counter(sampleRate float32, bucket string, n ...int) {
	CurrentStatter.Counter(sampleRate, bucket, n...)
}

// Timing writes a timer value to the CurrentStatter.  By default, values are
// simply logged.
func Timing(sampleRate float32, bucket string, d ...time.Duration) {
	CurrentStatter.Timing(sampleRate, bucket, d...)
}

// Gauge writes a static value to the CurrentStatter.  By default, values are
// simply logged.
func Gauge(sampleRate float32, bucket string, value ...string) {
	CurrentStatter.Gauge(sampleRate, bucket, value...)
}

// SetLogger updates the Logger object used by CurrentLogger and CurrentContext.
func SetLogger(logger Logger) Logger {
	if logger == nil {
		logger = NewIoLogger(nil)
	}

	CurrentLogger = logger
	CurrentContext.Logger = logger

	return logger
}

// NewContext returns a new Context object with the given key/value data.
func NewContext(data Data) *Context {
	return CurrentContext.New(data)
}

// AddContext adds the key and value to the CurrentContext's data.
func AddContext(key string, value interface{}) {
	CurrentContext.Add(key, value)
}

// DeleteContext removes the key from the CurrentContext's data.
func DeleteContext(key string) {
	CurrentContext.Delete(key)
}

// SetStatter sets up a basic Statter in the CurrentContext.  This Statter will
// be used by any Timer created from this Context.
func SetStatter(statter Statter, sampleRate float32, bucket string) {
	CurrentContext.SetStatter(statter, sampleRate, bucket)
}

// NewTimer creates a new Timer with the given key/value data.
func NewTimer(data Data) *Timer {
	return CurrentContext.Timer(data)
}

// SetTimeUnit sets the default time unit for the CurrentContext.  This gets
// passed down to Timer objects created from this Context.
func SetTimeUnit(unit string) {
	CurrentContext.TimeUnit = unit
}

// TimeUnit returns the default time unit for the CurrentContext.
func TimeUnit() string {
	return CurrentContext.TimeUnit
}

// SetErrorReporter sets the ErrorReporter used by the CurrentContext.  This
// will skip the default logging of the reported errors.
func SetErrorReporter(reporter ErrorReporter) {
	CurrentContext.ErrorReporter = reporter
}
