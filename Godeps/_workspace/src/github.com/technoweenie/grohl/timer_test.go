package grohl

import (
	"testing"
)

func TestTimerLog(t *testing.T) {
	context, buf := setupLogger(t)
	context.Add("a", "1")
	timer := context.Timer(Data{"b": "2"})
	timer.Log(Data{"c": "3"})

	buf.AssertLogged("a=1 b=2 at=start\na=1 b=2 c=3 elapsed=0.000")
}

func TestTimerLogInMS(t *testing.T) {
	context, buf := setupLogger(t)
	context.Add("a", "1")
	timer := context.Timer(Data{"b": "2"})
	timer.TimeUnit = "ms"
	timer.Log(Data{"c": "3"})

	expected := "a=1 b=2 at=start\na=1 b=2 c=3 elapsed=0.001"
	checkedLen := len(expected) - 3
	if result := buf.String(); result[0:checkedLen] != expected[0:checkedLen] {
		t.Errorf("Bad log output: %s", result)
	}
}

func TestTimerFinish(t *testing.T) {
	context, buf := setupLogger(t)
	context.Add("a", "1")
	timer := context.Timer(Data{"b": "2"})
	timer.Finish()

	buf.AssertLogged("a=1 b=2 at=start\na=1 b=2 at=finish elapsed=0.000")
}

func TestTimerWithStatter(t *testing.T) {
	context, buf := setupLogger(t)
	context.Add("a", "1")
	timer := context.Timer(Data{"b": "2"})
	statter := NewContext(nil)
	statter.Logger = context.Logger
	timer.SetStatter(statter, 1.0, "bucket")
	timer.Finish()

	expected := "a=1 b=2 at=start\n"
	expected = expected + "metric=bucket timing=0\n"
	expected = expected + "a=1 b=2 at=finish elapsed=0.000"
	buf.AssertLogged(expected)
}

func TestTimerWithContextStatter(t *testing.T) {
	context, buf := setupLogger(t)
	context.Add("a", "1")
	context.SetStatter(context, 1.0, "bucket")
	timer := context.Timer(Data{"b": "2"})
	timer.StatterBucket = "bucket2"
	timer.Finish()

	expected := "a=1 b=2 at=start\n"
	expected = expected + "a=1 metric=bucket2 timing=0\n"
	expected = expected + "a=1 b=2 at=finish elapsed=0.000"
	buf.AssertLogged(expected)

	if context.StatterBucket == "bucket2" {
		t.Errorf("Context's stat bucket was changed")
	}
}

func TestTimerWithNilStatter(t *testing.T) {
	oldlogger := CurrentContext.Logger

	context, buf := setupLogger(t)
	context.Add("a", "1")
	CurrentContext.Logger = context.Logger
	timer := context.Timer(Data{"b": "2"})
	timer.SetStatter(nil, 1.0, "bucket")
	timer.Finish()

	CurrentContext.Logger = oldlogger

	expected := "a=1 b=2 at=start\n"
	expected = expected + "metric=bucket timing=0\n"
	expected = expected + "a=1 b=2 at=finish elapsed=0.000"
	buf.AssertLogged(expected)
}
