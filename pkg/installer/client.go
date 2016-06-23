package installer

import (
	"crypto/rsa"
	"encoding/json"
	"errors"
	"reflect"
	"sync"
	"time"
)

// Cluster manages a cluster for a specific cloud provider
type Cluster interface {
	LaunchSteps() []Step
	DestroySteps() []Step
}

var DEFAULT_SSH_KEY_NAME = "flynn"

type SSHKey struct {
	Name       string
	PublicKey  []byte
	PrivateKey *rsa.PrivateKey
}

// ErrUnknownClusterType is returned if data.type doesn't match a registered cluster type
var ErrUnknownClusterType = errors.New("Unknown type of cluster")

var clusterMapping = map[string]Cluster{}
var clusterMappingMux sync.Mutex

// Register maps a type string with an implementor of Cluster
func Register(typ string, cluster Cluster) {
	clusterMappingMux.Lock()
	defer clusterMappingMux.Unlock()
	clusterMapping[typ] = cluster
}

type clusterType struct {
	Type string `json:"type"`
}

// UnmarshalCluster returns a Cluster for provided JSON
func UnmarshalCluster(data []byte) (Cluster, error) {
	clusterMappingMux.Lock()
	defer clusterMappingMux.Unlock()
	typ := &clusterType{}
	if err := json.Unmarshal(data, &typ); err != nil {
		return nil, err
	}
	example, ok := clusterMapping[typ.Type]
	if !ok {
		return nil, ErrUnknownClusterType
	}
	cluster := reflect.Indirect(reflect.New(reflect.TypeOf(example))).Interface()
	if err := json.Unmarshal(data, cluster); err != nil {
		return nil, err
	}
	return cluster.(Cluster), nil
}

// Step is used for each step of an operation
// such as launching or destroying a cluster
type Step struct {
	Description string
	Func        func(EventContext) error
}

// EventContext is passed to each Step function
type EventContext interface {
	Events() <-chan *Event
	// Done returns a chan which gets closed at end of operation
	Done() <-chan struct{}
	Log(lvl LogLevel, msg string)
	YesNoPrompt(msg string) bool
	ChoicePrompt(msg string, opts []ChoicePromptOption) *ChoicePromptOption
	SSHKeysPrompt() []*SSHKey
	SendOutput(string, interface{})
}

// LaunchCluster launches the provided Cluster
func LaunchCluster(cluster Cluster) <-chan *Event {
	ec := NewEventContext()
	return ec.Events()
}

// DestroyCluster destroys the provided Cluster
func DestroyCluster(cluster Cluster) <-chan *Event {
	ec := NewEventContext()
	return ec.Events()
}

type eventContext struct {
	ch    chan *Event
	done  chan struct{}
	id    int64
	idMux sync.Mutex
}

func NewEventContext() *eventContext {
	return &eventContext{
		ch:   make(chan *Event),
		done: make(chan struct{}),
	}
}

func (ec *eventContext) Events() <-chan *Event {
	return ec.ch
}

func (ec *eventContext) NextID() int64 {
	ec.idMux.Lock()
	defer ec.idMux.Unlock()
	ec.id++
	return ec.id
}

func (ec *eventContext) Done() <-chan struct{} {
	return ec.done
}

func (ec *eventContext) Log(lvl LogLevel, msg string) {
	ec.SendEvent(&Event{
		Type: EventTypeLog,
		Payload: &LogEvent{
			Level:   lvl,
			Message: msg,
		},
	})
}

func (ec *eventContext) SendOutput(name string, value interface{}) {
	ec.SendEvent(&Event{
		Type: EventTypeOutput,
		Payload: &OutputEvent{
			Name:  name,
			Value: value,
		},
	})
}

func (ec *eventContext) SendEvent(e *Event) {
	e.ID = ec.NextID()
	e.Timestamp = time.Now()
	ec.ch <- e
}

// EventType holds the type of an Event
type EventType string

var (
	// EventTypeLog is used for a log Event
	EventTypeLog EventType = "log"
	// EventTypePrompt is used to ask for data
	EventTypePrompt EventType = "prompt"
	// EventTypeOutput is used to send output values
	EventTypeOutput EventType = "output"
)

// Event holds different types of data passed back to the caller
type Event struct {
	Type      EventType   `json:"type"`
	ID        int64       `json:"id"`
	Timestamp time.Time   `json:"timestamp"`
	Payload   interface{} `json:"payload"`
}

type LogLevel string

var (
	LogLevelInfo  LogLevel = "info"
	LogLevelDebug LogLevel = "debug"
	LogLevelError LogLevel = "error"
)

type LogEvent struct {
	Level   LogLevel `json:"level"`
	Message string   `json:"message"`
}

type OutputEvent struct {
	Name  string      `json:"name"`
	Value interface{} `jsno:"value"`
}

// Prompt is used to ask user for input
type Prompt interface {
	ID() string
	Type() PromptType
	Respond(interface{})
	ResponseExample() interface{}
}
