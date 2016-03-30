package installer

import (
	"crypto/rsa"
	"fmt"
	"sync"
	"time"
)

const AuthenticationFailureCredentialPromptMessage = "Authentication failed, please select a new credential to continue"

type Step struct {
	Description string
	StepFunc    StepFunc
}

type StepFunc func(client *Client, progress chan<- int) error

type Cluster interface {
	LaunchSteps() []Step
	DestroySteps() []Step
}

// TODO(jvatic): Specify this interface
type Credential interface {
}

type Client struct {
	events    []*Event
	eventsMux sync.Mutex

	subs    map[chan<- *Event]*subscription
	subsMux sync.Mutex

	conf ClientConfig
}

type ClientConfig struct {
	AuthCallback func() ([]*SSHKey, error)
	Prompt       func(*Prompt) (PromptResponse, error)
}

type SSHKey struct {
	Name       string
	PublicKey  []byte
	PrivateKey *rsa.PrivateKey
}

type subscription struct {
	Since time.Time
}

func New() *Client {
	return NewWithConfig(ClientConfig{
		AuthCallback: func() ([]*SSHKey, error) {
			return nil, fmt.Errorf("installer: no auth callback given")
		},
		Prompt: func(p *Prompt) (PromptResponse, error) {
			return nil, fmt.Errorf("installer: no prompt callback given")
		},
	})
}

func NewWithConfig(conf ClientConfig) *Client {
	return &Client{
		events: make([]*Event, 0),
		subs:   make(map[chan<- *Event]*subscription),
		conf:   conf,
	}
}

func (c *Client) SSHKeys() ([]*SSHKey, error) {
	return c.conf.AuthCallback()
}

func (c *Client) LaunchCluster(cluster Cluster) error {
	return c.runSteps(cluster.LaunchSteps(), ProgressEventTypeClusterLaunch)
}

func (c *Client) DestroyCluster(cluster Cluster) error {
	return c.runSteps(cluster.DestroySteps(), ProgressEventTypeClusterDestroy)
}

func (c *Client) runSteps(steps []Step, eventType ProgressEventType) error {
	var n int
	total := len(steps)
	sendProgressEvent := func() {
		n++
		c.SendEvent(&Event{
			Body: ProgressEvent{
				Type:    eventType,
				Percent: (n * 100 / total * 100) / 100,
			},
		})
	}
	for _, step := range steps {
		c.SendLogEvent(step.Description)
		if err := step.StepFunc(c); err != nil {
			return err
		}
		sendProgressEvent()
	}
	return nil
}
