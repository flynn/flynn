package installer

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/cznic/ql"
	"github.com/flynn/flynn/pkg/random"
)

func (prompt *Prompt) Resolve(res *Prompt) {
	prompt.Resolved = true
	prompt.resChan <- res
}

func (event *Event) EventID() string {
	return event.ID
}

type Subscription struct {
	LastEventID string
	EventChan   chan *Event
	DoneChan    chan struct{}

	sendEventsMtx sync.Mutex
}

func (sub *Subscription) SendEvents(i *Installer) {
	sub.sendEventsMtx.Lock()
	defer sub.sendEventsMtx.Unlock()
	for _, event := range i.GetEventsSince(sub.LastEventID) {
		sub.LastEventID = event.ID
		sub.EventChan <- event
	}
}

func (i *Installer) Subscribe(eventChan chan *Event, lastEventID string) {
	i.subscribeMtx.Lock()
	defer i.subscribeMtx.Unlock()

	subscription := &Subscription{
		LastEventID: lastEventID,
		EventChan:   eventChan,
	}

	go func() {
		subscription.SendEvents(i)
	}()

	i.subscriptions = append(i.subscriptions, subscription)
}

func (i *Installer) GetEventsSince(eventID string) []*Event {
	events := make([]*Event, 0, len(i.events))
	var ts time.Time
	if eventID != "" {
		nano, err := strconv.ParseInt(strings.TrimPrefix(eventID, "event-"), 10, 64)
		if err != nil {
			i.logger.Debug(fmt.Sprintf("Error parsing event id: %s", err.Error()))
		} else {
			ts = time.Unix(0, nano)
		}
	}
	rows, err := i.db.Query(`SELECT ID, ClusterID, PromptID, Type, Timestamp, Description FROM events WHERE Timestamp > $1 AND DeletedAt IS NULL ORDER BY Timestamp`, ts)
	if err != nil {
		i.logger.Debug(fmt.Sprintf("GetEventsSince SQL Error: %s", err.Error()))
		return events
	}
	for rows.Next() {
		event := &Event{}
		if err := rows.Scan(&event.ID, &event.ClusterID, &event.PromptID, &event.Type, &event.Timestamp, &event.Description); err != nil {
			i.logger.Debug(fmt.Sprintf("GetEventsSince Scan Error: %s", err.Error()))
			continue
		}
		if event.Type == "log" {
			if c, err := i.FindCluster(event.ClusterID); err != nil || (err == nil && c.State == "running") {
				continue
			}
		}
		if event.Type == "new_cluster" || event.Type == "install_done" {
			event.Cluster, err = i.FindCluster(event.ClusterID)
			if err != nil {
				i.logger.Debug(fmt.Sprintf("GetEventsSince Error finding cluster %s: %s", event.ClusterID, err.Error()))
				continue
			}
		}
		if event.PromptID != "" {
			p := &Prompt{}
			if err := i.db.QueryRow(`SELECT ID, Type, Message, Yes, Input, Resolved FROM prompts WHERE ID == $1 AND DeletedAt IS NULL`, event.PromptID).Scan(&p.ID, &p.Type, &p.Message, &p.Yes, &p.Input, &p.Resolved); err != nil {
				i.logger.Debug(fmt.Sprintf("GetEventsSince Prompt Scan Error: %s", err.Error()))
				continue
			}
			event.Prompt = p
		}
		events = append(events, event)
	}
	return events
}

func (i *Installer) SendEvent(event *Event) {
	event.Timestamp = time.Now()
	event.ID = fmt.Sprintf("event-%d", event.Timestamp.UnixNano())

	if event.Type == "prompt" {
		if event.Prompt == nil {
			i.logger.Debug(fmt.Sprintf("SendEvent Error: Invalid prompt event: %v", event))
			return
		}
		event.PromptID = event.Prompt.ID
	}

	if event.Type == "error" {
		i.logger.Error(fmt.Sprintf("Error: %s", event.Description))
	} else {
		i.logger.Info(fmt.Sprintf("Event: %s: %s", event.Type, event.Description))
	}

	err := i.dbInsertItem("events", event)
	if err != nil {
		i.logger.Debug(err.Error())
	}

	for _, sub := range i.subscriptions {
		go sub.SendEvents(i)
	}
}

func (c *BaseCluster) findPrompt(id string) (*Prompt, error) {
	if c.pendingPrompt != nil && c.pendingPrompt.ID == id {
		return c.pendingPrompt, nil
	}
	return nil, errors.New("Prompt not found")
}

func (c *BaseCluster) sendPrompt(prompt *Prompt) *Prompt {
	c.pendingPrompt = prompt

	if err := c.installer.dbInsertItem("prompts", prompt); err != nil {
		c.installer.logger.Debug(fmt.Sprintf("sendPrompt db insert error: %s", err.Error()))
	}

	c.sendEvent(&Event{
		Type:      "prompt",
		ClusterID: c.ID,
		Prompt:    prompt,
	})

	res := <-prompt.resChan
	prompt.Resolved = true
	prompt.Yes = res.Yes
	prompt.Input = res.Input

	if err := c.dbUpdatePrompt(prompt); err != nil {
		c.installer.logger.Debug(fmt.Sprintf("sendPrompt db update error: %s", err.Error()))
		return res
	}

	c.sendEvent(&Event{
		Type:      "prompt",
		ClusterID: c.ID,
		Prompt:    prompt,
	})

	return res
}

func (c *BaseCluster) dbUpdatePrompt(prompt *Prompt) error {
	c.installer.dbMtx.Lock()
	defer c.installer.dbMtx.Unlock()

	return c.installer.txExec(`UPDATE prompts SET Resolved = $1, Yes = $2, Input = $3 WHERE ID == $4`, prompt.Resolved, prompt.Yes, prompt.Input, prompt.ID)
}

func (i *Installer) dbInsertItem(tableName string, item interface{}) error {
	i.dbMtx.Lock()
	defer i.dbMtx.Unlock()

	fields, err := ql.Marshal(item)
	if err != nil {
		return err
	}
	vStr := make([]string, 0, len(fields))
	for idx := range fields {
		vStr = append(vStr, fmt.Sprintf("$%d", idx+1))
	}
	list, err := ql.Compile(fmt.Sprintf(`
    INSERT INTO %s VALUES(%s);
	`, tableName, strings.Join(vStr, ", ")))
	if err != nil {
		return err
	}
	return i.txExec(list.String(), fields...)
}

func (c *BaseCluster) YesNoPrompt(msg string) bool {
	res := c.sendPrompt(&Prompt{
		ID:      random.Hex(16),
		Type:    "yes_no",
		Message: msg,
		resChan: make(chan *Prompt),
		cluster: c,
	})
	return res.Yes
}

func (c *BaseCluster) PromptInput(msg string) string {
	res := c.sendPrompt(&Prompt{
		ID:      random.Hex(16),
		Type:    "input",
		Message: msg,
		resChan: make(chan *Prompt),
		cluster: c,
	})
	return res.Input
}

func (c *BaseCluster) sendEvent(event *Event) {
	c.installer.SendEvent(event)
}

func (c *BaseCluster) SendLog(description string) {
	c.sendEvent(&Event{
		Type:        "log",
		ClusterID:   c.ID,
		Description: description,
	})
}

func (c *BaseCluster) SendError(err error) {
	c.sendEvent(&Event{
		Type:        "error",
		ClusterID:   c.ID,
		Description: err.Error(),
	})
}

func (c *BaseCluster) handleDone() {
	if c.State != "running" {
		return
	}
	c.sendEvent(&Event{
		Type:      "install_done",
		ClusterID: c.ID,
		Cluster:   c,
	})
	msg, err := c.DashboardLoginMsg()
	if err != nil {
		panic(err)
	}
	c.installer.logger.Info(msg)
}
