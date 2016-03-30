package installer

import "time"

type EventType string

var (
	EventTypeLog      EventType = "log"
	EventTypeProgress EventType = "progress"
)

type Event struct {
	Type      EventType
	Timestamp time.Time
	Body      interface{}
}

type LogEvent struct {
	Message string
}

type ProgressEventType string

var (
	ProgressEventTypeClusterLaunch  ProgressEventType = "cluster_launch"
	ProgressEventTypeClusterDestroy ProgressEventType = "cluster_destroy"
)

type ProgressEvent struct {
	Type        ProgressEventType
	Description string
	Percent     int
}

func (c *Client) Subscribe(ch chan<- *Event, since time.Time) {
	sub := &subscription{
		Since: since,
	}

	c.subsMux.Lock()
	c.subs[ch] = sub
	c.subsMux.Unlock()

	c.eventsMux.Lock()
	for _, e := range c.events {
		c.sendEvent(e, ch, sub.Since)
	}
	c.eventsMux.Unlock()
}

func (c *Client) Unsubscribe(ch chan<- *Event) {
	c.subsMux.Lock()
	defer c.subsMux.Unlock()
	delete(c.subs, ch)
}

func (c *Client) SendLogEvent(msg string) {
	c.SendEvent(&Event{
		Type: EventTypeLog,
		Body: LogEvent{msg},
	})
}

func (c *Client) SendPromptEvent(p *Prompt) PromptResponse {
	c.SendEvent(&Event{})
}

func (c *Client) SendEvent(e *Event) {
	if e.Timestamp.IsZero() {
		e.Timestamp = time.Now()
	}

	c.eventsMux.Lock()
	c.events = append(c.events, e)
	c.eventsMux.Unlock()

	c.subsMux.Lock()
	defer c.subsMux.Unlock()
	for ch, sub := range c.subs {
		c.sendEvent(e, ch, sub.Since)
	}
}

func (c *Client) sendEvent(e *Event, ch chan<- *Event, since time.Time) {
	if !e.Timestamp.After(since) {
		return
	}
	ch <- e
}
