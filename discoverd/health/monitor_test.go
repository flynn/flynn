package health

import (
	"errors"
	"time"

	. "github.com/flynn/go-check"
)

type MonitorSuite struct{}

var _ = Suite(&MonitorSuite{})

type CheckFunc func() error

func (f CheckFunc) Check() error { return f() }

func (MonitorSuite) TestMonitor(c *C) {
	type step struct {
		up    bool
		event MonitorStatus
	}

	checker := func(steps []step, threshold int) (chan MonitorEvent, chan MonitorEvent) {
		var i int
		var finished bool
		done := make(chan struct{})
		expectedEvents := make(chan MonitorEvent, 1)
		actualEvents := make(chan MonitorEvent)

		check := CheckFunc(func() error {
			if finished {
				return errors.New("finished")
			}
			defer func() {
				if i >= len(steps) {
					done <- struct{}{}
					// ensure the stream has been closed before returning
					<-done
					finished = true
				}
			}()

			step := steps[i]
			i++

			if !step.up {
				err := errors.New("check failure")
				if step.event > 0 {
					expectedEvents <- MonitorEvent{
						Status: step.event,
						Err:    err,
					}
				}
				return err
			}
			if step.event > 0 {
				expectedEvents <- MonitorEvent{Status: step.event}
			}
			return nil
		})

		stream := Monitor{
			Threshold:     threshold,
			StartInterval: time.Nanosecond,
			Interval:      time.Nanosecond,
		}.Run(check, actualEvents)
		go func() {
			<-done
			stream.Close()
			done <- struct{}{}
		}()

		return expectedEvents, actualEvents
	}

	for _, t := range []struct {
		name      string
		steps     []step
		threshold int
	}{
		{
			name:  "service doesn't come up",
			steps: []step{{}, {}, {}},
		},
		{
			name: "service comes up right away",
			steps: []step{
				{event: MonitorStatusUp, up: true},
				{up: true},
				{up: true},
			},
		},
		{
			name: "service comes up after a few checks",
			steps: []step{
				{}, {}, {},
				{event: MonitorStatusUp, up: true},
			},
		},
		{
			name: "up/down/up - default threshold",
			steps: []step{
				{event: MonitorStatusUp, up: true},
				{},
				{event: MonitorStatusDown},
				{up: true},
				{event: MonitorStatusUp, up: true},
			},
		},
		{
			name:      "up/down/up - custom threshold",
			threshold: 3,
			steps: []step{
				{event: MonitorStatusUp, up: true},
				{},
				{},
				{event: MonitorStatusDown},
				{up: true},
				{up: true},
				{event: MonitorStatusUp, up: true},
			},
		},
		{
			name: "flapping - alternate",
			steps: []step{
				{event: MonitorStatusUp, up: true},
				{},
				{up: true},
				{},
				{up: true},
				{},
				{event: MonitorStatusDown},
				{up: true},
				{},
				{up: true},
				{},
			},
		},
		{
			name:      "flapping - consecutive",
			threshold: 3,
			steps: []step{
				{event: MonitorStatusUp, up: true},
				{},
				{},
				{up: true},
				{},
				{},
				{up: true},
				{},
				{},
				{event: MonitorStatusDown},
				{up: true},
				{up: true},
				{},
				{up: true},
				{up: true},
				{},
			},
		},
	} {
		c.Log(t.name)

		expectedEvents, actualEvents := checker(t.steps, t.threshold)
		for actual := range actualEvents {
			select {
			case expected := <-expectedEvents:
				// functions are not comparable, so we check it and then nil it
				c.Assert(actual.Check, FitsTypeOf, CheckFunc(nil))
				actual.Check = nil
				c.Assert(actual, DeepEquals, expected)
			default:
				c.Fatalf("unexpected event %#v", actual)
			}
		}
	}
}
