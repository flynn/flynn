package installer

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/awslabs/aws-sdk-go/aws"
	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/awslabs/aws-sdk-go/gen/cloudformation"
	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/awslabs/aws-sdk-go/gen/ec2"
	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/awslabs/aws-sdk-go/gen/route53"
	"github.com/flynn/flynn/pkg/awsutil"
	"github.com/flynn/flynn/pkg/sshkeygen"
	"github.com/flynn/flynn/util/release/types"
)

var DisallowedEC2InstanceTypes = []string{"t1.micro", "t2.micro", "t2.small", "m1.small"}
var DefaultInstanceType = "m3.medium"
var StackNotFoundError = errors.New("Stack does not exist")

func (c *AWSCluster) saveField(field string, value interface{}) error {
	c.cluster.installer.dbMtx.Lock()
	defer c.cluster.installer.dbMtx.Unlock()

	tx, err := c.cluster.installer.db.Begin()
	if err != nil {
		return err
	}
	_, err = tx.Exec(fmt.Sprintf(`
  UPDATE aws_clusters SET %s = $2 WHERE ClusterID == $1
  `, field), c.ClusterID, value)
	if err != nil {
		tx.Rollback()
		return err
	}
	return tx.Commit()
}

func (c *AWSCluster) SetDefaultsAndValidate() error {
	if c.InstanceType == "" {
		c.InstanceType = DefaultInstanceType
	}

	if c.VpcCidr == "" {
		c.VpcCidr = "10.0.0.0/16"
	}

	if c.SubnetCidr == "" {
		c.SubnetCidr = "10.0.0.0/21"
	}

	if err := c.validateInputs(); err != nil {
		return err
	}

	if err := c.cluster.SetDefaultsAndValidate(); err != nil {
		return err
	}

	c.ec2 = ec2.New(c.creds, c.Region, nil)
	c.cf = cloudformation.New(c.creds, c.Region, nil)
	return nil
}

func (c *AWSCluster) validateInputs() error {
	if c.Region == "" {
		return fmt.Errorf("No region specified")
	}

	for _, t := range DisallowedEC2InstanceTypes {
		if c.InstanceType == t {
			return fmt.Errorf("Unsupported instance type %s", c.InstanceType)
		}
	}

	return nil
}

func (c *AWSCluster) Run() {
	go func() {
		defer func() {
			c.cluster.handleDone()
		}()

		steps := []func() error{
			c.createKeyPair,
			c.cluster.allocateDomain,
			c.fetchImageID,
			c.createStack,
			c.fetchStackOutputs,
			c.configureDNS,
			c.bootstrap,
		}

		for _, step := range steps {
			if err := step(); err != nil {
				if c.cluster.State != "deleting" {
					c.cluster.setState("error")
					c.cluster.SendError(err)
				}
				return
			}
		}

		c.cluster.setState("running")

		if err := c.cluster.configureCLI(); err != nil {
			c.cluster.SendInstallLogEvent(fmt.Sprintf("WARNING: Failed to configure CLI: %s", err))
		}
	}()
}

func (c *AWSCluster) Delete() {
	c.cf = cloudformation.New(c.creds, c.Region, nil)
	go func() {
		stackEventsSince := time.Now()
		c.cluster.setState("deleting")
		if err := c.fetchStack(); err != StackNotFoundError {
			fmt.Println(err)
			if err := c.cf.DeleteStack(&cloudformation.DeleteStackInput{
				StackName: aws.String(c.StackName),
			}); err != nil {
				c.cluster.setState("error")
				c.cluster.SendError(fmt.Errorf("Unable to delete stack %s: %s", c.StackName, err))
			} else {
				if err := c.waitForStackCompletion("DELETE", stackEventsSince); err != nil {
					c.cluster.SendError(err)
				}
			}
		}
		if err := c.cluster.RemoveFromDB(); err != nil {
			c.cluster.SendError(err)
		}
		c.cluster.sendEvent(&Event{
			ClusterID:   c.cluster.ID,
			Type:        "cluster_state",
			Description: "deleted",
		})
	}()
}

func (c *AWSCluster) loadKeyPair(name string) error {
	keypair, err := loadSSHKey(name)
	if err != nil {
		return err
	}
	fingerprint, err := awsutil.FingerprintImportedKey(keypair.PrivateKey)
	if err != nil {
		return err
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
		return err
	}
	if len(res.KeyPairs) == 0 {
		return errors.New("No matching key found")
	}
	c.cluster.SSHKey = keypair
	for _, p := range res.KeyPairs {
		if *p.KeyName == name {
			c.cluster.SSHKeyName = name
			return nil
		}
	}
	c.cluster.SSHKeyName = *res.KeyPairs[0].KeyName
	return saveSSHKey(c.cluster.SSHKeyName, keypair)
}

func (c *AWSCluster) createKeyPair() error {
	keypairName := "flynn"
	if c.cluster.SSHKeyName != "" {
		keypairName = c.cluster.SSHKeyName
	}
	if err := c.loadKeyPair(keypairName); err == nil {
		c.cluster.SendInstallLogEvent(fmt.Sprintf("Using saved key pair (%s)", c.cluster.SSHKeyName))
		return nil
	}

	c.cluster.SendInstallLogEvent("Creating key pair")
	keypair, err := sshkeygen.Generate()
	if err != nil {
		return err
	}

	enc := base64.StdEncoding
	publicKeyBytes := make([]byte, enc.EncodedLen(len(keypair.PublicKey)))
	enc.Encode(publicKeyBytes, keypair.PublicKey)

	res, err := c.ec2.ImportKeyPair(&ec2.ImportKeyPairRequest{
		KeyName:           aws.String(keypairName),
		PublicKeyMaterial: publicKeyBytes,
	})
	if apiErr, ok := err.(aws.APIError); ok && apiErr.Code == "InvalidKeyPair.Duplicate" {
		if c.cluster.YesNoPrompt(fmt.Sprintf("Key pair %s already exists, would you like to delete it?", keypairName)) {
			c.cluster.SendInstallLogEvent("Deleting key pair")
			if err := c.ec2.DeleteKeyPair(&ec2.DeleteKeyPairRequest{
				KeyName: aws.String(keypairName),
			}); err != nil {
				return err
			}
			return c.createKeyPair()
		} else {
			for {
				keypairName = c.cluster.PromptInput("Please enter a new key pair name")
				if keypairName != "" {
					c.cluster.SSHKeyName = keypairName
					return c.createKeyPair()
				}
			}
		}
	}
	if err != nil {
		return err
	}

	c.cluster.SSHKey = keypair
	c.cluster.SSHKeyName = *res.KeyName

	err = saveSSHKey(keypairName, keypair)
	if err != nil {
		return err
	}
	return nil
}

func (c *AWSCluster) fetchImageID() (err error) {
	defer func() {
		if err == nil {
			return
		}
		c.cluster.SendInstallLogEvent(err.Error())
		if c.ImageID != "" {
			c.cluster.SendInstallLogEvent("Falling back to saved Image ID")
			err = nil
			return
		}
		return
	}()

	c.cluster.SendInstallLogEvent("Fetching image manifest")

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
		return errors.New(fmt.Sprintf("No image found for region %s", c.Region))
	}
	c.ImageID = imageID
	if err := c.saveField("ImageID", imageID); err != nil {
		return err
	}
	return nil
}

func (c *AWSCluster) fetchLatestEC2Images() ([]*release.EC2Image, error) {
	client := &http.Client{}
	resp, err := client.Get("https://dl.flynn.io/ec2/images.json")
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, errors.New(fmt.Sprintf("Failed to fetch list of flynn images: %s", resp.Status))
	}
	dec := json.NewDecoder(resp.Body)
	manifest := release.EC2Manifest{}
	err = dec.Decode(&manifest)
	if err != nil {
		return nil, err
	}
	if len(manifest.Versions) == 0 {
		return nil, errors.New("No versions in manifest")
	}
	return manifest.Versions[0].Images, nil
}

type stackTemplateData struct {
	Instances           []struct{}
	DefaultInstanceType string
}

func (c *AWSCluster) createStack() error {
	c.cluster.SendInstallLogEvent("Generating start script")
	startScript, discoveryToken, err := c.cluster.genStartScript(c.cluster.NumInstances)
	if err != nil {
		return err
	}
	c.cluster.DiscoveryToken = discoveryToken
	if err := c.cluster.saveField("DiscoveryToken", discoveryToken); err != nil {
		return err
	}

	var stackTemplateBuffer bytes.Buffer
	err = stackTemplate.Execute(&stackTemplateBuffer, &stackTemplateData{
		Instances:           make([]struct{}, c.cluster.NumInstances),
		DefaultInstanceType: DefaultInstanceType,
	})
	if err != nil {
		return err
	}
	stackTemplateString := stackTemplateBuffer.String()

	parameters := []cloudformation.Parameter{
		{
			ParameterKey:   aws.String("ImageId"),
			ParameterValue: aws.String(c.ImageID),
		},
		{
			ParameterKey:   aws.String("ClusterDomain"),
			ParameterValue: aws.String(c.cluster.Domain.Name),
		},
		{
			ParameterKey:   aws.String("KeyName"),
			ParameterValue: aws.String(c.cluster.SSHKeyName),
		},
		{
			ParameterKey:   aws.String("UserData"),
			ParameterValue: aws.String(startScript),
		},
		{
			ParameterKey:   aws.String("InstanceType"),
			ParameterValue: aws.String(c.InstanceType),
		},
		{
			ParameterKey:   aws.String("VpcCidrBlock"),
			ParameterValue: aws.String(c.VpcCidr),
		},
		{
			ParameterKey:   aws.String("SubnetCidrBlock"),
			ParameterValue: aws.String(c.SubnetCidr),
		},
	}

	stackEventsSince := time.Now()

	if c.StackID != "" && c.StackName != "" {
		if err := c.fetchStack(); err == nil && !strings.HasPrefix(*c.stack.StackStatus, "DELETE") {
			if c.cluster.YesNoPrompt(fmt.Sprintf("Stack found from previous installation (%s), would you like to delete it? (a new one will be created either way)", c.StackName)) {
				c.cluster.SendInstallLogEvent(fmt.Sprintf("Deleting stack %s", c.StackName))
				if err := c.cf.DeleteStack(&cloudformation.DeleteStackInput{
					StackName: aws.String(c.StackName),
				}); err != nil {
					c.cluster.SendInstallLogEvent(fmt.Sprintf("Unable to delete stack %s: %s", c.StackName, err))
				}
			}
		}
	}

	c.cluster.SendInstallLogEvent("Creating stack")
	res, err := c.cf.CreateStack(&cloudformation.CreateStackInput{
		OnFailure:        aws.String("DELETE"),
		StackName:        aws.String(c.StackName),
		Tags:             []cloudformation.Tag{},
		TemplateBody:     aws.String(stackTemplateString),
		TimeoutInMinutes: aws.Integer(10),
		Parameters:       parameters,
	})
	if err != nil {
		return err
	}
	c.StackID = *res.StackID

	if err := c.saveField("StackID", c.StackID); err != nil {
		return err
	}
	return c.waitForStackCompletion("CREATE", stackEventsSince)
}

type StackEventSort []cloudformation.StackEvent

func (e StackEventSort) Len() int {
	return len(e)
}

func (e StackEventSort) Swap(i, j int) {
	e[i], e[j] = e[j], e[i]
}

func (e StackEventSort) Less(i, j int) bool {
	return e[j].Timestamp.After(e[i].Timestamp)
}

func (c *AWSCluster) waitForStackCompletion(action string, after time.Time) error {
	stackID := aws.String(c.StackID)

	actionCompleteSuffix := "_COMPLETE"
	actionFailureSuffix := "_FAILED"
	actionInProgressSuffix := "_IN_PROGRESS"
	isComplete := false
	isFailed := false

	stackEvents := make([]cloudformation.StackEvent, 0)
	var nextToken aws.StringValue

	var fetchStackEvents func() error
	fetchStackEvents = func() error {
		res, err := c.cf.DescribeStackEvents(&cloudformation.DescribeStackEventsInput{
			NextToken: nextToken,
			StackName: stackID,
		})
		if err != nil {
			switch err.(type) {
			case *url.Error:
				return nil
			default:
				return err
			}
		}

		// some events are not returned in order
		sort.Sort(StackEventSort(res.StackEvents))

		for _, se := range res.StackEvents {
			if !se.Timestamp.After(after) {
				continue
			}
			stackEventExists := false
			for _, e := range stackEvents {
				if *e.EventID == *se.EventID {
					stackEventExists = true
					break
				}
			}
			if stackEventExists {
				continue
			}
			stackEvents = append(stackEvents, se)
			if se.ResourceType != nil && se.ResourceStatus != nil {
				if *se.ResourceType == "AWS::CloudFormation::Stack" {
					if strings.HasSuffix(*se.ResourceStatus, actionInProgressSuffix) && !strings.HasPrefix(*se.ResourceStatus, action) {
						isFailed = true
						break
					}
					if strings.HasSuffix(*se.ResourceStatus, actionCompleteSuffix) {
						if strings.HasPrefix(*se.ResourceStatus, action) {
							isComplete = true
						} else {
							isFailed = true
						}
					} else if strings.HasSuffix(*se.ResourceStatus, actionFailureSuffix) {
						isFailed = true
					}
				}
				var desc string
				if se.ResourceStatusReason != nil {
					desc = fmt.Sprintf(" (%s)", *se.ResourceStatusReason)
				}
				name := *se.ResourceType
				if se.LogicalResourceID != nil {
					name = fmt.Sprintf("%s (%s)", name, *se.LogicalResourceID)
				}
				c.cluster.SendInstallLogEvent(fmt.Sprintf("%s\t%s%s", name, *se.ResourceStatus, desc))
			}
		}
		if res.NextToken != nil {
			nextToken = res.NextToken
			fetchStackEvents()
		}

		return nil
	}

	for {
		if err := fetchStackEvents(); err != nil {
			return err
		}
		if isComplete {
			break
		}
		if isFailed {
			return fmt.Errorf("Failed to create stack %s", c.StackName)
		}
		time.Sleep(1 * time.Second)
	}

	return nil
}

func (c *AWSCluster) fetchStack() error {
	stackID := aws.String(c.StackID)

	c.cluster.SendInstallLogEvent("Fetching stack")
	res, err := c.cf.DescribeStacks(&cloudformation.DescribeStacksInput{
		StackName: stackID,
	})
	if err != nil {
		return err
	}
	if len(res.Stacks) == 0 {
		return StackNotFoundError
	}
	stack := &res.Stacks[0]
	if strings.HasPrefix(*stack.StackStatus, "DELETE_") {
		return StackNotFoundError
	}
	c.stack = stack
	return nil
}

func (c *AWSCluster) fetchStackOutputs() error {
	c.fetchStack()

	instanceIPs := make([]string, 0, c.cluster.NumInstances)
	for _, o := range c.stack.Outputs {
		v := *o.OutputValue
		if strings.HasPrefix(*o.OutputKey, "IPAddress") {
			instanceIPs = append(instanceIPs, v)
		}
		if *o.OutputKey == "DNSZoneID" {
			c.DNSZoneID = v
		}
	}
	if int64(len(instanceIPs)) != c.cluster.NumInstances {
		return fmt.Errorf("expected stack outputs to include %d instance IPs but found %d", c.cluster.NumInstances, len(instanceIPs))
	}
	c.cluster.InstanceIPs = instanceIPs

	if c.DNSZoneID == "" {
		return fmt.Errorf("stack outputs do not include DNSZoneID")
	}

	if err := c.saveField("DNSZoneID", c.DNSZoneID); err != nil {
		return err
	}
	if err := c.cluster.saveField("InstanceIPs", c.cluster.InstanceIPs); err != nil {
		return err
	}

	return nil
}

func (c *AWSCluster) configureDNS() error {
	// TODO(jvatic): Run directly after receiving zone create complete stack event
	c.cluster.SendInstallLogEvent("Configuring DNS")

	// Set region to us-east-1, since any other region will fail for global services like Route53
	r53 := route53.New(c.creds, "us-east-1", nil)
	res, err := r53.GetHostedZone(&route53.GetHostedZoneRequest{ID: aws.String(c.DNSZoneID)})
	if err != nil {
		return err
	}
	if err := c.cluster.Domain.Configure(res.DelegationSet.NameServers); err != nil {
		return err
	}
	return nil
}

func (c *AWSCluster) bootstrap() error {
	if c.stack == nil {
		return errors.New("No stack found")
	}
	return c.cluster.bootstrap()
}
