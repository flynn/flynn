package installer

import (
	"encoding/json"
	"errors"
	"io"
	"reflect"
	"sync"
	"time"
)

// Cluster manages a cluster for a specific cloud provider
type Cluster interface {
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
	cluster := reflect.New(reflect.TypeOf(example)).Interface()
	if err := json.Unmarshal(data, &cluster); err != nil {
		return nil, err
	}
	return cluster.(Cluster), nil
}

// LaunchCluster launches the provided Cluster
func LaunchCluster(cluster Cluster) <-chan *Event {
	ec := &eventContext{
		ch: make(chan *Event),
	}
	return ec.ch
}

// DestroyCluster destroys the provided Cluster
func DestroyCluster(cluster Cluster) <-chan *Event {
	ec := &eventContext{
		ch: make(chan *Event),
	}
	return ec.ch
}

type eventContext struct {
	ch    chan *Event
	id    int64
	idMux sync.Mutex
}

func (ec *eventContext) NextID() int64 {
	ec.idMux.Lock()
	defer ec.idMux.Unlock()
	ec.id++
	return ec.id
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
)

// Event holds different types of data passed back to the caller
type Event struct {
	Type      EventType   `json:"type"`
	ID        int64       `json:"id"`
	Timestamp time.Time   `json:"timestamp"`
	Payload   interface{} `json:"payload"`
}

// Prompt is used to ask user for input
type Prompt interface {
	ID() string
	Respond(io.Reader)
}
