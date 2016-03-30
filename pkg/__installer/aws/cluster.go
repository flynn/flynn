package awscluster

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/flynn/flynn/pkg/awsutil"
	"github.com/flynn/flynn/pkg/installer"
	domain "github.com/flynn/flynn/pkg/installer/domain"
	"github.com/flynn/flynn/pkg/sshkeygen"
	"github.com/flynn/flynn/util/release/types"
	"src/github.com/awslabs/aws-sdk-go/aws"
	"src/github.com/awslabs/aws-sdk-go/gen/cloudformation"
	"src/github.com/awslabs/aws-sdk-go/gen/ec2"
)

type stackEventType int

const (
	stackEventTypeSingleton stackEventType = iota
	stackEventTypePerInstance
)

var stackEvents = map[string]stackEventType{
	"AWS::EC2::VPC":                         stackEventTypeSingleton,
	"AWS::EC2::InternetGateway":             stackEventTypeSingleton,
	"AWS::EC2::VPCGatewayAttachment":        stackEventTypeSingleton,
	"AWS::EC2::RouteTable":                  stackEventTypeSingleton,
	"AWS::EC2::Route":                       stackEventTypeSingleton,
	"AWS::EC2::Subnet":                      stackEventTypeSingleton,
	"AWS::EC2::SubnetRouteTableAssociation": stackEventTypeSingleton,
	"AWS::EC2::SecurityGroup":               stackEventTypeSingleton,
	"AWS::EC2::Instance":                    stackEventTypePerInstance,
	"AWS::Route53::HealthCheck":             stackEventTypePerInstance,
	"AWS::Route53::RecordSetGroup":          stackEventTypeSingleton,
	"AWS::Route53::HostedZone":              stackEventTypeSingleton,
}

type AWSCluster struct {
	basecluster.BaseCluster

	// inputs
	InstanceType string
	VpcCIDR      string
	SubnetCIDR   string

	SSHKeyName string
	SSHKey     *installer.SSHKey
	ImageID    string
	StackID    string

	startScript string

	creds   installer.Credential
	servers []installer.TargetServer
	cf      *cloudformation.CloudFormation
	ec2     *ec2.EC2
}

func (c *AWSCluster) LaunchSteps() []installer.Step {
	return []installer.Step{
		{"Validating input", c.preLaunchValidationStep},
		{"Creating key pair", c.createKeyPairStep},
		{"Allocating domain", c.allocateDomainStep},
		{"Fetching image manifest", c.fetchImageIDStep},
		{"Generating start script", c.generateStartScriptStep},
		{"Creating stack", c.createStackStep},
		{"Fetching stack outputs", c.fetchStackOutputsStep},
		{"Configuring DNS", c.configureDNSStep},
		{"Uploading backup", c.uploadBackupStep},
		{"Running bootstrap", c.bootstrapStep},
		{"Waiting for DNS to propagate", c.waitForDNSStep},
	}
}

func (c *AWSCluster) DestroySteps() []installer.Step {
	return []installer.Step{
		{"Verifying stack", c.verifyStackExistsStep},
		{"Destroying stack", c.destroyStackStep},
	}
}

func (c *AWSCluster) preLaunchValidationStep(ic *installer.Client) error {
	// TODO(jvatic): Make sure all required fields are set
	return nil
}

func (c *AWSCluster) findAWSKeyPair(key *installer.SSHKey) (*installer.SSHKey, error) {
	fingerprint, err := awsutil.FingerprintImportedKey(key.PrivateKey)
	if err != nil {
		return nil, err
	}
	res, err := c.ec2.DescribeKeyPairs(&ec2.DescribeKeyPairsRequest{
		Filters: []ec2.Filter{
			{
				Name:   aws.String("fingerprint"),
				Values: []string{fingerprint},
			},
		},
	})
	if err != nil {
		return nil, err
	}
	if len(res.KeyPairs) == 0 {
		return nil, fmt.Errorf("awscluster: no keys matching %s (%s) found", fingerprint, key.Name)
	}
	for _, p := range res.KeyPairs {
		if *p.KeyName == key.Name {
			return key, nil
		}
	}
	return &installer.SSHKey{
		Name:       *res.KeyPairs[0].KeyName,
		PublicKey:  key.PublicKey,
		PrivateKey: key.PrivateKey,
	}, nil
}

func (c *AWSCluster) createKeyPairStep(ic *installer.Client) error {
	sshKeys, err := ic.SSHKeys()
	if err != nil {
		return err
	}

	if c.SSHKeyName != "" {
		newSSHKeys := make([]*installer.SSHKey, len(sshKeys))
		idx := -1
		for i, key := range sshKeys {
			if key.Name == c.SSHKeyName {
				idx = i
				newSSHKeys[0] = key
				break
			}
		}
		for i, key := range sshKeys {
			if i == idx {
				continue
			}
			if i < idx {
				i++
			}
			newSSHKeys[i] = key
		}
	}
	for _, key := range sshKeys {
		if k, err := c.findAWSKeyPair(key); err == nil {
			c.SSHKeyName = k.Name
			ic.SendLogEvent(fmt.Sprintf("Using saved key pair (%s)", k.Name))
			return nil
		}
	}

	keyName := "flynn"
	if c.SSHKeyName != "" {
		keyName = c.SSHKeyName
	}

	var key *installer.SSHKey
	for _, k := range sshKeys {
		if k.Name == keyName {
			key = k
			break
		}
	}

	if key == nil {
		ic.SendLogEvent(fmt.Sprintf("Creating key pair (%s)", keyName))
		k, err := sshkeygen.Generate()
		if err != nil {
			return err
		}
		key = &installer.SSHKey{
			Name:       keyName,
			PublicKey:  k.PublicKey,
			PrivateKey: k.PrivateKey,
		}
	} else {
		ic.SendLogEvent(fmt.Sprintf("Importing key pair (%s)", key.Name))
	}

	enc := base64.StdEncoding
	publicKeyBytes := make([]byte, enc.EncodedLen(len(key.PublicKey)))
	enc.Encode(publicKeyBytes, key.PublicKey)

	var res *ec2.ImportKeyPairResult
	err = c.wrapRequest(ic, func() error {
		var err error
		res, err = c.ec2.ImportKeyPair(&ec2.ImportKeyPairRequest{
			KeyName:           aws.String(keyName),
			PublicKeyMaterial: publicKeyBytes,
		})
		return err
	})
	if apiErr, ok := err.(aws.APIError); ok && apiErr.Code == "InvalidKeyPair.Duplicate" {
		yes, err := ic.YesNoPrompt(fmt.Sprintf("Key pair %s already exists, would you like to delete it?", key.Name))
		if err != nil {
			return err
		}
		if yes {
			ic.SendLogEvent(fmt.Sprintf("Deleting key pair (%s)", key.Name))
			if err := c.wrapRequest(ic, func() error {
				return c.ec2.DeleteKeyPair(&ec2.DeleteKeyPairRequest{
					KeyName: aws.String(keyName),
				})
			}); err != nil {
				return err
			}
			return c.createKeyPairStep(ic)
		}
		for {
			keyName, err = ic.InputPrompt("Please enter a new key pair name")
			if err != nil {
				return err
			}
			if keyName != "" {
				c.SSHKeyName = keyName
				return c.createKeyPairStep(ic)
			}
		}
	}
	if err != nil {
		return err
	}

	c.SSHKey = key
	c.SSHKeyName = key.Name

	return nil
}

func (c *AWSCluster) allocateDomainStep(ic *installer.Client) error {
	var err error
	c.Domain, err = domain.Allocate()
	return err
}

func (c *AWSCluster) fetchImageIDStep(ic *installer.Client) error {
	latestImages, err := c.fetchLatestEC2Images()
	if err != nil {
		return err
	}
	var imageID string
	for _, i := range latestImages {
		if i.Region == c.Region {
			imageID = i.ID
			break
		}
	}
	if imageID == "" {
		return fmt.Errorf("No image found for region %s", c.Region)
	}
	c.ImageID = imageID
	return nil
}

func (c *AWSCluster) fetchLatestEC2Images() ([]*release.EC2Image, error) {
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
	return manifest.Versions[0].Images, nil
}

func (c *AWSCluster) generateStartScriptStep(ic *installer.Client) error {
	startScript, discoveryToken, err := c.GenerateStartScript("/dev/xvdb")
	if err != nil {
		return err
	}
	c.startScript = startScript
	c.DiscoveryToken = discoveryToken
	return nil
}

type stackTemplateData struct {
	Instances           []struct{}
	DefaultInstanceType string
}

func (c *AWSCluster) createStackStep(ic *installer.Client) error {
	var stackTemplateBuffer bytes.Buffer
	err = stackTemplate.Execute(&stackTemplateBuffer, &stackTemplateData{
		Instances:           make([]struct{}, c.NumInstances),
		DefaultInstanceType: DefaultInstanceType,
	})
	if err != nil {
		return err
	}
	stackTemplateString := stackTemplateBuffer.String()

	stackEventsSince := time.Now()

	var res *cloudformation.CreateStackResult
	err = c.wrapRequest(func() error {
		var err error
		res, err = c.cf.CreateStack(&cloudformation.CreateStackInput{
			OnFailure:        aws.String("DELETE"),
			StackName:        aws.String(c.StackName),
			Tags:             []cloudformation.Tag{},
			TemplateBody:     aws.String(stackTemplateString),
			TimeoutInMinutes: aws.Integer(10),
			Parameters: []cloudformation.Parameter{
				{
					ParameterKey:   aws.String("ImageId"),
					ParameterValue: aws.String(c.ImageID),
				},
				{
					ParameterKey:   aws.String("ClusterDomain"),
					ParameterValue: aws.String(c.Domain.Name),
				},
				{
					ParameterKey:   aws.String("KeyName"),
					ParameterValue: aws.String(c.SSHKeyName),
				},
				{
					ParameterKey:   aws.String("UserData"),
					ParameterValue: aws.String(c.startScript),
				},
				{
					ParameterKey:   aws.String("InstanceType"),
					ParameterValue: aws.String(c.InstanceType),
				},
				{
					ParameterKey:   aws.String("VpcCidrBlock"),
					ParameterValue: aws.String(c.VpcCIDR),
				},
				{
					ParameterKey:   aws.String("SubnetCidrBlock"),
					ParameterValue: aws.String(c.SubnetCIDR),
				},
			},
		})
		return err
	})
	if err != nil {
		return err
	}
	c.StackID = *res.StackID

	return c.waitForStackCompletion("CREATE", stackEventsSince)
}

func (c *AWSCluster) fetchStackOutputsStep(ic *installer.Client) error {
	return nil
}

func (c *AWSCluster) configureDNSStep(ic *installer.Client) error {
	return nil
}

func (c *AWSCluster) uploadBackupStep(ic *installer.Client) error {
	return nil
}

func (c *AWSCluster) bootstrapStep(ic *installer.Client) error {
	return nil
}

func (c *AWSCluster) waitForDNSStep(ic *installer.Client) error {
	return nil
}

func (c *AWSCluster) verifyStackExistsStep(ic *installer.Client) error {
	return nil
}

func (c *AWSCluster) destroyStackStep(ic *installer.Client) error {
	return nil
}

func (c *AWSCluster) wrapRequest(ic *installer.Client, runRequest func() error) error {
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
		} else if apiErr, ok := err.(aws.APIError); ok && (apiErr.StatusCode == 401 || apiErr.Code == "InvalidClientTokenId") {
			if authAttemptsRemaining > 0 {
				if creds, err := ic.CredentialPrompt(installer.AuthenticationFailureCredentialPromptMessage); err == nil {
					c.creds = creds
					authAttemptsRemaining--
					continue
				}
			}
		}
		return err
	}
}
