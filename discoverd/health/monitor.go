package health

import (
	"time"

	"github.com/flynn/flynn/pkg/stream"
)

type MonitorConfig struct {
	// StartInterval is the check interval to use when waiting for the service
	// to transition from created -> up. It defaults to 100ms.
	StartInterval time.Duration

	// Interval is the check interval to use when the service is up or down. It
	// defaults to two seconds.
	Interval time.Duration

	// Threshold is the number of consecutive checks of the same status before
	// a service will transition up -> down or down -> up. It defaults to 2.
	Threshold int
}

type MonitorStatus int

const (
	MonitorStatusUnknown MonitorStatus = iota
	MonitorStatusCreated
	MonitorStatusUp
	MonitorStatusDown
)

type MonitorEvent struct {
	Status MonitorStatus
	// If Status is MonitorStatusDown, Err is the last failure
	Err error
	// Check is included to identify the monitor.
	Check Check
}

const (
	defaultStartInterval = 100 * time.Millisecond
	defaultInterval      = 2 * time.Second
	defaultThreshold     = 2
)

// Monitor monitors a service using Check and sends up/down transitions to ch
func Monitor(cfg MonitorConfig, check Check, ch chan MonitorEvent) stream.Stream {
	if cfg.StartInterval == 0 {
		cfg.StartInterval = defaultStartInterval
	}
	if cfg.Interval == 0 {
		cfg.Interval = defaultInterval
	}
	if cfg.Threshold == 0 {
		cfg.Threshold = defaultThreshold
	}

	stream := stream.New()
	go func() {
		t := time.NewTicker(cfg.StartInterval)
		defer close(ch)

		status := MonitorStatusCreated
		var upCount, downCount int
		up := func() {
			downCount = 0
			upCount++
			if status == MonitorStatusCreated || status == MonitorStatusDown && upCount >= cfg.Threshold {
				if status == MonitorStatusCreated {
					t.Stop()
					t = time.NewTicker(cfg.Interval)
				}
				status = MonitorStatusUp
				ch <- MonitorEvent{
					Status: status,
					Check:  check,
				}
			}
		}
		down := func(err error) {
			upCount = 0
			downCount++
			if status == MonitorStatusUp && downCount >= cfg.Threshold {
				status = MonitorStatusDown
				ch <- MonitorEvent{
					Status: status,
					Err:    err,
					Check:  check,
				}
			}
		}
		check := func() {
			if err := check.Check(); err != nil {
				down(err)
			} else {
				up()
			}
		}

		check()
	outer:
		for {
			select {
			case <-t.C:
				check()
			case <-stream.StopCh:
				break outer
			}
		}
		t.Stop()
	}()

	return stream
}
