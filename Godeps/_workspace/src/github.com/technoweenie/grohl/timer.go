package grohl

import (
	"time"
)

// A Timer tracks the duration spent since its creation.
type Timer struct {
	Started  time.Time
	TimeUnit string
	context  *Context
	*_statter
}

// Creates a Timer from the current Context, with the given key/value data.
func (c *Context) Timer(data Data) *Timer {
	context := c.New(data)
	context.Log(Data{"at": "start"})
	return &Timer{
		Started:  time.Now(),
		TimeUnit: context.TimeUnit,
		context:  context,
		_statter: c._statter.dup(),
	}
}

// Finish writes a final log message with the elapsed time shown.
func (t *Timer) Finish() {
	t.Log(Data{"at": "finish"})
}

// Log writes a log message with extra data or the elapsed time shown.  Pass nil
// or use Finish() if there is no extra data.
func (t *Timer) Log(data Data) error {
	if data == nil {
		data = make(Data)
	}

	dur := t.Elapsed()

	if _, ok := data["elapsed"]; !ok {
		data["elapsed"] = t.durationUnit(dur)
	}

	t._statter.Timing(dur)
	return t.context.Log(data)
}

// Elapsed returns the duration since the Timer was created.
func (t *Timer) Elapsed() time.Duration {
	return time.Since(t.Started)
}

func (t *Timer) durationUnit(dur time.Duration) float64 {
	sec := dur.Seconds()
	if t.TimeUnit == "ms" {
		return sec * 1000
	}
	return sec
}
