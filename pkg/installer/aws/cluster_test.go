package awscluster

import (
	"errors"
	"sync"
	"testing"

	"github.com/awslabs/aws-sdk-go/gen/cloudformation"
	"github.com/flynn/flynn/pkg/installer"
	"github.com/flynn/flynn/pkg/sshkeygen"
	. "github.com/flynn/go-check"
)

func newFakeAWSClient() *fakeAWSClient {
	return &fakeAWSClient{
		responses:   map[string][][]interface{}{},
		invocations: map[string]int{},
	}
}

type fakeAWSClient struct {
	responses      map[string][][]interface{}
	responsesMux   sync.Mutex
	invocations    map[string]int
	invocationsMux sync.Mutex
}

func (aws *fakeAWSClient) getResponse(name string) []interface{} {
	aws.responsesMux.Lock()
	defer aws.responsesMux.Unlock()
	if aws.responses[name] == nil || len(aws.responses[name]) == 0 {
		return nil
	}
	args := aws.responses[name][0]
	aws.responses[name] = aws.responses[name][1:]
	return args
}

func (aws *fakeAWSClient) invocation(name string) {
	aws.invocationsMux.Lock()
	defer aws.invocationsMux.Unlock()
	n := aws.invocations[name]
	aws.invocations[name] = n + 1
}

func (aws *fakeAWSClient) AddResponse(name string, args []interface{}) {
	aws.responsesMux.Lock()
	defer aws.responsesMux.Unlock()
	if aws.responses[name] == nil {
		aws.responses[name] = make([][]interface{}, 0, 1)
	}
	aws.responses[name] = append(aws.responses[name], args)
}

func (aws *fakeAWSClient) AssertInvocations(c *C, name string, n int) {
	aws.invocationsMux.Lock()
	defer aws.invocationsMux.Unlock()
	c.Assert(aws.invocations[name], Equals, n)
}

func (aws *fakeAWSClient) CheckKeyPairExists(key *installer.SSHKey) (string, error) {
	aws.invocation("CheckKeyPairExists")
	args := aws.getResponse("CheckKeyPairExists")
	if args == nil {
		return "", nil
	}
	if args[1] == nil {
		return args[0].(string), nil
	}
	return args[0].(string), args[1].(error)
}

func (aws *fakeAWSClient) ImportKeyPair(ctx installer.EventContext, key *installer.SSHKey) error {
	aws.invocation("ImportKeyPair")
	return nil
}

func (aws *fakeAWSClient) DeleteKeyPair(name string) error {
	return nil
}

func (aws *fakeAWSClient) FetchStack(name string) (*AWSStack, error) {
	return nil, nil
}

func (aws *fakeAWSClient) DeleteStack(name string) error {
	return nil
}

func (aws *fakeAWSClient) CreateStack(input *cloudformation.CreateStackInput) (*AWSStack, error) {
	return nil, nil
}

func (aws *fakeAWSClient) StreamStackEvents(stackName string) (<-chan *AWSStackEvent, <-chan error) {
	events := make(chan *AWSStackEvent)
	errors := make(chan error)
	// TODO
	return events, errors
}

func (aws *fakeAWSClient) GetHostedZoneNameServers(zoneID string) ([]string, error) {
	// TODO
	return []string{}, nil
}

// Hook gocheck up to the "go test" runner
func Test(t *testing.T) { TestingT(t) }

type S struct {
}

var _ = Suite(&S{})

func (s *S) SetUpSuite(c *C) {
}

func newEventResponder(ec installer.EventContext) *eventResponder {
	done := make(chan struct{})
	er := &eventResponder{
		PromptResponses: []interface{}{},
		done:            done,
	}
	go func() {
		ch := ec.Events()
		for {
			select {
			case e := <-ch:
				er.eventsMux.Lock()
				er.events = append(er.events, e)
				er.eventsMux.Unlock()
				er.promptResponsesMux.Lock()
				if e.Type == installer.EventTypePrompt && len(er.PromptResponses) > 0 {
					e.Payload.(installer.Prompt).Respond(er.PromptResponses[0])
					er.PromptResponses = er.PromptResponses[1:]
				}
				er.promptResponsesMux.Unlock()
			case <-done:
				return
			}
		}
	}()
	return er
}

type eventResponder struct {
	PromptResponses    []interface{}
	promptResponsesMux sync.Mutex
	events             []*installer.Event
	eventsMux          sync.Mutex
	done               chan struct{}
}

func (er *eventResponder) AddPromptResponse(res interface{}) {
	er.promptResponsesMux.Lock()
	defer er.promptResponsesMux.Unlock()
	er.PromptResponses = append(er.PromptResponses, res)
}

func (er *eventResponder) AssertEvents(c *C, expected []installer.EventType) {
	er.eventsMux.Lock()
	defer er.eventsMux.Unlock()
	actual := make([]installer.EventType, len(er.events))
	for i, e := range er.events {
		actual[i] = e.Type
	}
	c.Assert(actual, DeepEquals, expected)
	er.events = []*installer.Event{}
}

func (er *eventResponder) Done() {
	er.done <- struct{}{}
}

func (s *S) TestResolveSSHKeyStepCreatesKey(c *C) {
	awsClient := newFakeAWSClient()
	cluster := &Cluster{
		AWSClient: awsClient,
	}
	ec := installer.NewEventContext()
	eventResponder := newEventResponder(ec)
	eventResponder.AddPromptResponse(installer.SSHKeysPromptResponse{[]*installer.SSHKey{}})
	err := cluster.resolveSSHKeyStep(ec)
	c.Assert(err, IsNil)
	eventResponder.Done()
	eventResponder.AssertEvents(c, []installer.EventType{
		installer.EventTypePrompt,
		installer.EventTypeLog,
		installer.EventTypeOutput,
	})
	awsClient.AssertInvocations(c, "CheckKeyPairExists", 0)
	awsClient.AssertInvocations(c, "ImportKeyPair", 1)
}

func (s *S) TestResolveSSHKeyStepImportsKey(c *C) {
	awsClient := newFakeAWSClient()
	cluster := &Cluster{
		AWSClient: awsClient,
	}
	k, err := sshkeygen.Generate()
	c.Assert(err, IsNil)
	key := &installer.SSHKey{
		Name:       installer.DEFAULT_SSH_KEY_NAME,
		PublicKey:  k.PublicKey,
		PrivateKey: k.PrivateKey,
	}
	ec := installer.NewEventContext()
	eventResponder := newEventResponder(ec)
	eventResponder.AddPromptResponse(installer.SSHKeysPromptResponse{[]*installer.SSHKey{key}})
	awsClient.AddResponse("CheckKeyPairExists", []interface{}{"", errors.New("Key pair doesn't exist")})
	err = cluster.resolveSSHKeyStep(ec)
	c.Assert(err, IsNil)
	eventResponder.Done()
	eventResponder.AssertEvents(c, []installer.EventType{
		installer.EventTypePrompt,
		installer.EventTypeLog,
		installer.EventTypeOutput,
	})
	awsClient.AssertInvocations(c, "CheckKeyPairExists", 1)
	awsClient.AssertInvocations(c, "ImportKeyPair", 1)
	c.Assert(cluster.sshKey, Not(IsNil))
	c.Assert(key.PublicKey, DeepEquals, cluster.sshKey.PublicKey)
}

func (s *S) TestResolveSSHKeyStepUsesExistingKey(c *C) {
	awsClient := newFakeAWSClient()
	cluster := &Cluster{
		AWSClient: awsClient,
	}
	k, err := sshkeygen.Generate()
	c.Assert(err, IsNil)
	key := &installer.SSHKey{
		Name:       installer.DEFAULT_SSH_KEY_NAME,
		PublicKey:  k.PublicKey,
		PrivateKey: k.PrivateKey,
	}
	ec := installer.NewEventContext()
	eventResponder := newEventResponder(ec)
	eventResponder.AddPromptResponse(installer.SSHKeysPromptResponse{[]*installer.SSHKey{key}})
	awsClient.AddResponse("CheckKeyPairExists", []interface{}{key.Name, nil})
	err = cluster.resolveSSHKeyStep(ec)
	c.Assert(err, IsNil)
	eventResponder.Done()
	eventResponder.AssertEvents(c, []installer.EventType{
		installer.EventTypePrompt,
		installer.EventTypeLog,
		installer.EventTypeOutput,
	})
	awsClient.AssertInvocations(c, "CheckKeyPairExists", 1)
	awsClient.AssertInvocations(c, "ImportKeyPair", 0)
	c.Assert(cluster.sshKey, Not(IsNil))
	c.Assert(key.PublicKey, DeepEquals, cluster.sshKey.PublicKey)
}
