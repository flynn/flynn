package awscluster

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"sync"
	"time"

	"github.com/awslabs/aws-sdk-go/aws"
	"github.com/awslabs/aws-sdk-go/gen/cloudformation"
	"github.com/awslabs/aws-sdk-go/gen/ec2"
	"github.com/awslabs/aws-sdk-go/gen/route53"
	"github.com/flynn/flynn/pkg/installer"
	"github.com/flynn/flynn/util/release/types"
)

func NewAWSClient(creds aws.CredentialsProvider, region string) AWSClient {
	return &awsClient{
		cf:  cloudformation.New(creds, region, nil),
		ec2: ec2.New(creds, region, nil),
		// Set region to us-east-1, since any other region will fail for global services like Route53
		r53:             route53.New(creds, "us-east-1", nil),
		stackEventsChan: map[string][]*awsStackEventSubscription{},
	}
}

// AWSClient handles communication with AWS
type AWSClient interface {
	EC2Images(region string) ([]*release.EC2Image, error)
	CheckKeyPairExists(key *installer.SSHKey) (string, error)
	ImportKeyPair(ctx installer.EventContext, key *installer.SSHKey) error
	DeleteKeyPair(name string) error
	FetchStack(name string) (*AWSStack, error)
	DeleteStack(name string) error
	CreateStack(*cloudformation.CreateStackInput) (*AWSStack, error)
	StreamStackEvents(stackName string, since time.Time) (<-chan *AWSStackEvent, <-chan error)
	WaitForStackEvent(stackName, resourceType, resourceStatus string, since time.Time) error
	GetHostedZoneNameServers(zoneID string) ([]string, error)
}

type AWSStack struct {
	Status  string
	Outputs map[string]string
}

type AWSStackEvent struct {
	EventID              string
	LogicalResourceID    string
	ResourceType         string
	ResourceStatus       string
	ResourceStatusReason string
	Timestamp            time.Time
}

type awsClient struct {
	cf                 *cloudformation.CloudFormation
	ec2                *ec2.EC2
	r53                *route53.Route53
	stackEventSubs     map[string][]*awsStackEventSubscription
	stackEventSubsMux  sync.Mutex
	stackEventClose    map[string]chan struct{}
	stackEventCloseMux sync.Mutex
}

type awsStackEventSubscription struct {
	Events chan<- *AWSStackEvent
	Errors chan<- error
	Since  time.Time
}

func stringFromPointer(str *string) string {
	if str == nil {
		return ""
	}
	return *str
}

func (aws *awsClient) wrapRequest(runRequest func() error) error {
	const rateExceededErrStr = "Rate exceeded"
	const maxBackoff = 10 * time.Second
	backoff := 1 * time.Second
	timeout := time.After(35 * time.Second)
	authAttemptsRemaining := 3
	for {
		err := runRequest()
		if err != nil && err.Error() == rateExceededErrStr {
			select {
			case <-time.After(backoff):
				backoff = backoff * 2
				if backoff > maxBackoff {
					backoff = maxBackoff
				}
				continue
			case <-timeout:
			}
		}
		return err
	}
}

func (aws *awsClient) EC2Images(region string) ([]*release.EC2Image, error) {
	res, err := http.Get("https://dl.flynn.io/ec2/images.json")
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Failed to fetch list of flynn images: %s", res.Status)
	}
	manifest := release.EC2Manifest{}
	if err := json.NewDecoder(res.Body).Decode(&manifest); err != nil {
		return nil, err
	}
	if len(manifest.Versions) == 0 {
		return nil, errors.New("No versions in manifest")
	}
	images := make([]*release.EC2Image, 0, len(manifest.Versions[0].Images))
	for _, i := range manifest.Versions[0].Images {
		if i.Region == region {
			images = append(images, i)
		}
	}
	return images, nil
}

func (aws *awsClient) CheckKeyPairExists(key *installer.SSHKey) (string, error) {
	// TODO
	return "", nil
}

func (aws *awsClient) ImportKeyPair(ctx installer.EventContext, key *installer.SSHKey) error {
	// TODO
	return nil
}

func (aws *awsClient) DeleteKeyPair(name string) error {
	// TODO
	return nil
}

func (aws *awsClient) FetchStack(name string) (*AWSStack, error) {
	// TODO
	return nil, nil
}

func (aws *awsClient) DeleteStack(name string) error {
	// TODO
	return nil
}

func (aws *awsClient) CreateStack(input *cloudformation.CreateStackInput) (*AWSStack, error) {
	// TODO
	return nil, nil
}

func (aws *awsClient) subscribeToStackEvents(stackName string, since time.Time, events chan<- *AWSStackEvent, errors chan<- error) *awsStackEventSubscription {
	aws.stackEventSubsMux.Lock()
	defer aws.stackEventSubsMux.Unlock()
	subs, ok := aws.stackEventsChan[stackName]
	if !ok {
		subs = make([]*awsStackEventSubscription)
		aws.stackEventCloseMux.Lock()
		done := make(chan struct{})
		aws.stackEventClose[stackName] = done
		aws.stackEventCloseMux.Unlock()
		go func() {
			if err := aws.streamStackEvents(stackName, done); err != nil {
				aws.stackEventCloseMux.Lock()
				delete(aws.stackEventClose, stackName)
				aws.stackEventCloseMux.Unlock()
				aws.handleEventStreamError(stackName, err)
			}
		}()
	}
	s := &awsStackEventSubscription{events, errors}
	subs = append(subs, s)
	aws.stackEventSubs[stackName] = subs
	return s
}

func (aws *awsClient) unsubscribeToStackEvents(stackName string, sub *awsStackEventSubscription) {
	aws.stackEventSubsMux.Lock()
	defer aws.stackEventSubsMux.Unlock()
	subs, ok := aws.stackEventsChan[stackName]
	if !ok {
		return
	}
	newSubs := make([]*awsStackEventSubscription, 0, len(subs))
	for _, s := range subs {
		if s != sub {
			newSubs = append(newSubs, s)
			aws.stackEventCloseMux.Lock()
			done := aws.stackEventClose[stackName]
			close(done)
			delete(aws.stackEventClose, stackName)
			aws.stackEventCloseMux.Unlock()
		}
	}
	if len(newSubs) == 0 {
		delete(aws.stackEventSubs, stackName)
	} else {
		aws.stackEventSubs[stackName] = newSubs
	}
}

func (aws *awsClient) handleStackEvent(stackName string, event *AWSStackEvent) {
	aws.stackEventSubsMux.Lock()
	defer aws.stackEventSubsMux.Unlock()
	subs, ok := aws.stackEventsChan[stackName]
	if !ok {
		return
	}
	for _, sub := range subs {
		// only accept events since given time
		if !event.Timestamp.After(sub.Since) {
			continue
		}
		sub.Events <- event
	}
}

func (aws *awsClient) handleEventStreamError(stackName string, err error) {
	aws.stackEventSubsMux.Lock()
	defer aws.stackEventSubsMux.Unlock()
	subs, ok := aws.stackEventsChan[stackName]
	if !ok {
		return
	}
	for _, sub := range subs {
		sub.Errors <- err
	}
	delete(aws.stackEventsChan, stackName)
}

type stackEventSort []cloudformation.StackEvent

func (e stackEventSort) Len() int {
	return len(e)
}

func (e stackEventSort) Swap(i, j int) {
	e[i], e[j] = e[j], e[i]
}

func (e stackEventSort) Less(i, j int) bool {
	return e[j].Timestamp.After(e[i].Timestamp)
}

func (aws *awsClient) streamStackEvents(stackName string, done <-chan struct{}) error {
	var stackEvents []*AWSStackEvent
	var nextToken aws.StringValue

	var fetchStackEvents func() error
	fetchStackEvents = func() error {
		res, err := c.cf.DescribeStackEvents(&cloudformation.DescribeStackEventsInput{
			NextToken: nextToken,
			StackName: &stackName,
		})
		if err != nil {
			return err
		}

		// some events are not returned in order
		sort.Sort(stackEventSort(res.StackEvents))

		for _, se := range res.StackEvents {
			event := &AWSStackEvent{
				EventID:              stringFromPointer(se.EventID),
				LogicalResourceID:    stringFromPointer(se.LogicalResourceID),
				ResourceType:         stringFromPointer(se.ResourceType),
				ResourceStatus:       stringFromPointer(se.ResourceStatus),
				ResourceStatusReason: stringFromPointer(se.ResourceStatusReason),
				Timestamp:            se.Timestamp,
			}

			// don't accept duplicate events
			stackEventExists := false
			for _, e := range stackEvents {
				if e.EventID == event.EventID {
					stackEventExists = true
					break
				}
			}
			if stackEventExists {
				continue
			}
			stackEvents = append(stackEvents, event)

			// send event to subscribers
			aws.handleStackEvent(stackName, event)
		}
		if res.NextToken != nil {
			nextToken = res.NextToken
			fetchStackEvents()
		}

		return nil
	}

	for {
		select {
		case <-done:
			return nil
		default:
			if err := aws.wrapRequest(fetchStackEvents); err != nil {
				return err
			}
		}
		time.Sleep(1 * time.Second)
	}
}

func (aws *awsClient) StreamStackEvents(stackName string, since time.Time) (<-chan *AWSStackEvent, <-chan error, chan struct{}) {
	events := make(chan *AWSStackEvent)
	errors := make(chan error)
	done := make(chan struct{})
	sub := aws.subscribeToStackEvents(stackName, since, events, errors)
	go func() {
		<-done
		aws.unsubscribeToStackEvents(stackName, sub)
	}()
	return events, errors, done
}

func (aws *awsClient) WaitForStackEvent(stackName, resourceType, resourceStatus string, since time.Time) error {
	events, errors, done := aws.StreamStackEvents(stackName)
	defer close(done)
	for {
		select {
		case err := <-errors:
			return err
		case e := <-events:
			if e.ResourceType == resourceType && e.ResourceStatus == resourceStatus {
				return nil
			}
		}
	}
}

func (aws *awsClient) GetHostedZoneNameServers(zoneID string) ([]string, error) {
	// TODO
	return []string{}, nil
}
