package awscluster

import (
	"time"

	"github.com/awslabs/aws-sdk-go/aws"
	"github.com/awslabs/aws-sdk-go/gen/cloudformation"
	"github.com/awslabs/aws-sdk-go/gen/ec2"
	"github.com/awslabs/aws-sdk-go/gen/route53"
	"github.com/flynn/flynn/pkg/installer"
)

func NewAWSClient(creds aws.CredentialsProvider, region string) AWSClient {
	return &awsClient{
		cf:  cloudformation.New(creds, region, nil),
		ec2: ec2.New(creds, region, nil),
		// Set region to us-east-1, since any other region will fail for global services like Route53
		r53: route53.New(creds, "us-east-1", nil),
	}
}

// AWSClient handles communication with AWS
type AWSClient interface {
	CheckKeyPairExists(key *installer.SSHKey) (string, error)
	ImportKeyPair(ctx installer.EventContext, key *installer.SSHKey) error
	DeleteKeyPair(name string) error
	FetchStack(name string) (*AWSStack, error)
	DeleteStack(name string) error
	CreateStack(*cloudformation.CreateStackInput) (*AWSStack, error)
	StreamStackEvents(stackName string) (<-chan *AWSStackEvent, <-chan error)
	GetHostedZoneNameServers(zoneID string) ([]string, error)
}

type AWSStack struct {
	Status  string
	Outputs map[string]string
}

type AWSStackEvent struct {
	LogicalResourceID    string
	ResourceType         string
	ResourceStatus       string
	ResourceStatusReason string
	Timestamp            time.Time
}

type awsClient struct {
	cf  *cloudformation.CloudFormation
	ec2 *ec2.EC2
	r53 *route53.Route53
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

func (aws *awsClient) StreamStackEvents(stackName string) (<-chan *AWSStackEvent, <-chan error) {
	events := make(chan *AWSStackEvent)
	errors := make(chan error)
	// TODO
	return events, errors
}

func (aws *awsClient) GetHostedZoneNameServers(zoneID string) ([]string, error) {
	// TODO
	return []string{}, nil
}
