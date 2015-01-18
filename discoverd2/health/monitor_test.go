package health

import (
	"errors"
	"time"

	. "github.com/flynn/flynn/Godeps/_workspace/src/github.com/flynn/go-check"
	"github.com/flynn/flynn/pkg/stream"
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

	var stream stream.Stream
	checker := func(steps []step) (chan MonitorEvent, Check) {
		var i int
		events := make(chan MonitorEvent, 1)
		return events, CheckFunc(func() error {
			defer func() {
				if i >= len(steps) {
					stream.Close()
				}
			}()

			step := steps[i]
			i++

			if !step.up {
				err := errors.New("check failure")
				if step.event > 0 {
					events <- MonitorEvent{
						Status: step.event,
						Err:    err,
					}
				}
				return err
			}
			if step.event > 0 {
				events <- MonitorEvent{Status: step.event}
			}
			return nil
		})
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

		expectedEvents, check := checker(t.steps)
		actualEvents := make(chan MonitorEvent)
		stream = Monitor(MonitorConfig{
			Threshold:     t.threshold,
			StartInterval: time.Nanosecond,
			Interval:      time.Nanosecond,
		}, check, actualEvents)

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
