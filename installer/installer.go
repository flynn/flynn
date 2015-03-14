package installer

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"text/template"
	"time"

	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/awslabs/aws-sdk-go/aws"
	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/awslabs/aws-sdk-go/gen/cloudformation"
	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/awslabs/aws-sdk-go/gen/ec2"
	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/awslabs/aws-sdk-go/gen/route53"
	"github.com/flynn/flynn/Godeps/_workspace/src/golang.org/x/crypto/ssh"
	cfg "github.com/flynn/flynn/cli/config"
	"github.com/flynn/flynn/pkg/etcdcluster"
	"github.com/flynn/flynn/pkg/sshkeygen"
	release "github.com/flynn/flynn/util/release"
)

type Event struct {
	Description string
}

var DisallowedEC2InstanceTypes = []string{"t1.micro", "t2.micro", "t2.small", "m1.small"}
var DefaultInstanceType = "m3.medium"

type Stack struct {
	Region       string                  `json:"region,omitempty"`
	NumInstances int                     `json:"num_instances,omitempty"`
	InstanceType string                  `json:"instance_type,omitempty"`
	Creds        aws.CredentialsProvider `json:"-"`
	YesNoPrompt  func(string) bool       `json:"-"`
	PromptInput  func(string) string     `json:"-"`

	ControllerKey       string  `json:"controller_key,omitempty"`
	ControllerPin       string  `json:"controller_pin,omitempty"`
	DashboardLoginToken string  `json:"dashboard_login_token,omitempty"`
	Domain              *Domain `json:"domain"`
	CACert              string  `json:"ca_cert"`

	EventChan chan *Event   `json:"-"`
	ErrChan   chan error    `json:"-"`
	Done      chan struct{} `json:"-"`

	ImageID        string                `json:"image_id,omitempty"`
	StackID        string                `json:"stack_id,omitempty"`
	StackName      string                `json:"stack_name,omitempty"`
	Stack          *cloudformation.Stack `json:"-"`
	SSHKey         *sshkeygen.SSHKey     `json:"-"`
	SSHKeyName     string                `json:"ssh_key_name,omitempty"`
	VpcCidr        string                `json:"vpc_cidr_block,omitempty"`
	SubnetCidr     string                `json:"subnet_cidr_block,omitempty"`
	DiscoveryToken string                `json:"discovery_token"`
	InstanceIPs    []string              `json:"instance_ips,omitempty"`
	DNSZoneID      string                `json:"dns_zone_id,omitempty"`

	persistMutex sync.Mutex

	cf  *cloudformation.CloudFormation
	ec2 *ec2.EC2
}

func (s *Stack) setDefaults() {
	if s.NumInstances == 0 {
		s.NumInstances = 1
	}

	if s.InstanceType == "" {
		s.InstanceType = DefaultInstanceType
	}

	if s.VpcCidr == "" {
		s.VpcCidr = "10.0.0.0/16"
	}

	if s.SubnetCidr == "" {
		s.SubnetCidr = "10.0.0.0/21"
	}
}

func (s *Stack) validateInputs() error {
	if s.NumInstances <= 0 {
		return fmt.Errorf("You must specify at least one instance")
	}

	if s.Region == "" {
		return fmt.Errorf("No region specified")
	}

	if s.NumInstances > 5 {
		return fmt.Errorf("Maximum of 5 instances exceeded")
	}

	if s.NumInstances == 2 {
		return fmt.Errorf("You must specify 1 or 3+ instances, not 2")
	}

	for _, t := range DisallowedEC2InstanceTypes {
		if s.InstanceType == t {
			return fmt.Errorf("Unsupported instance type %s", s.InstanceType)
		}
	}

	return nil
}

func (s *Stack) ClusterAddCmd() (string, error) {
	if s.ControllerKey == "" || s.ControllerPin == "" || s.Domain == nil || s.Domain.Name == "" {
		return "", fmt.Errorf("Not enough data present")
	}
	return fmt.Sprintf("flynn cluster add -g %[1]s:2222 -p %[2]s default https://controller.%[1]s %[3]s", s.Domain.Name, s.ControllerPin, s.ControllerKey), nil
}

func (s *Stack) ClusterConfig() *cfg.Cluster {
	return &cfg.Cluster{
		Name:    s.StackName,
		URL:     "https://controller." + s.Domain.Name,
		Key:     s.ControllerKey,
		GitHost: fmt.Sprintf("%s:2222", s.Domain.Name),
		TLSPin:  s.ControllerPin,
	}
}

func (s *Stack) DashboardLoginMsg() (string, error) {
	if s.DashboardLoginToken == "" || s.Domain == nil || s.Domain.Name == "" {
		return "", fmt.Errorf("Not enough data present")
	}
	return fmt.Sprintf("The built-in dashboard can be accessed at http://dashboard.%s with login token %s", s.Domain.Name, s.DashboardLoginToken), nil
}

func (s *Stack) promptUseExistingStack(savedStack *Stack) bool {
	if s.StackID == "" || s.StackName == "" || savedStack.NumInstances != s.NumInstances || savedStack.InstanceType != s.InstanceType || savedStack.Region != s.Region {
		return false
	}

	if err := s.fetchStack(); err != nil {
		return false
	}

	if !s.YesNoPrompt(fmt.Sprintf("It appears you already have a cluster of this configuration (stack %s), would you like to continue?", s.StackName)) {
		s.Domain = savedStack.Domain
		s.DashboardLoginToken = savedStack.DashboardLoginToken
		s.CACert = savedStack.CACert
		return true
	}
	return false
}

func (s *Stack) RunAWS() error {
	s.setDefaults()
	if err := s.validateInputs(); err != nil {
		return err
	}

	s.EventChan = make(chan *Event)
	s.ErrChan = make(chan error)
	s.Done = make(chan struct{})
	s.InstanceIPs = make([]string, 0, s.NumInstances)
	s.ec2 = ec2.New(s.Creds, s.Region, nil)
	s.cf = cloudformation.New(s.Creds, s.Region, nil)

	savedStack := &Stack{}
	savedStack.load()
	s.StackID = savedStack.StackID
	s.StackName = savedStack.StackName
	s.SSHKeyName = savedStack.SSHKeyName

	go func() {
		defer close(s.Done)

		if s.promptUseExistingStack(savedStack) {
			return
		}

		steps := []func() error{
			s.createKeyPair,
			s.allocateDomain,
			s.fetchImageID,
			s.createStack,
			s.fetchStackOutputs,
			s.configureDNS,
			s.bootstrap,
		}

		for _, step := range steps {
			if err := step(); err != nil {
				s.persist()
				s.SendError(err)
				return
			}
			if err := s.persist(); err != nil {
				s.SendError(err)
			}
		}
	}()
	return nil
}

func (s *Stack) SendEvent(description string) {
	s.EventChan <- &Event{description}
}

func (s *Stack) SendError(err error) {
	s.ErrChan <- err
}

func (s *Stack) fetchImageID() (err error) {
	defer func() {
		if err == nil {
			return
		}
		s.SendEvent(err.Error())
		if s.ImageID != "" {
			s.SendEvent("Falling back to saved Image ID")
			err = nil
			return
		}
		return
	}()

	s.SendEvent("Fetching image manifest")

	latestVersion, err := fetchLatestVersion()
	if err != nil {
		return err
	}
	var imageID string
	for _, i := range latestVersion.Images {
		if i.Region == s.Region {
			imageID = i.ID
			break
		}
	}
	if imageID == "" {
		return errors.New(fmt.Sprintf("No image found for region %s", s.Region))
	}
	s.ImageID = imageID
	return nil
}

func (s *Stack) allocateDomain() error {
	s.SendEvent("Allocating domain")
	domain, err := AllocateDomain()
	if err != nil {
		return err
	}
	s.Domain = domain
	return nil
}

func (s *Stack) createKeyPair() error {
	keypairName := "flynn"
	if s.SSHKeyName != "" {
		keypairName = s.SSHKeyName
	}
	if keypair, err := loadSSHKey(keypairName); err == nil {
		s.SSHKey = keypair
		s.SSHKeyName = keypairName
		res, err := s.ec2.DescribeKeyPairs(&ec2.DescribeKeyPairsRequest{
			Filters: []ec2.Filter{{
				Name:   aws.String("key-name"),
				Values: []string{keypairName},
			}},
		})
		if err == nil && len(res.KeyPairs) > 0 && *res.KeyPairs[0].KeyName == keypairName {
			// key exists, we're good to go
			// TODO(jvatic): verify key fingerprint
			s.SendEvent(fmt.Sprintf("Using saved key pair (%s)", keypairName))
			return nil
		}
	}

	s.SendEvent("Creating key pair")
	keypair, err := sshkeygen.Generate()
	if err != nil {
		return err
	}

	enc := base64.StdEncoding
	publicKeyBytes := make([]byte, enc.EncodedLen(len(keypair.PublicKey)))
	enc.Encode(publicKeyBytes, keypair.PublicKey)

	res, err := s.ec2.ImportKeyPair(&ec2.ImportKeyPairRequest{
		KeyName:           aws.String(keypairName),
		PublicKeyMaterial: publicKeyBytes,
	})
	if apiErr, ok := err.(aws.APIError); ok && apiErr.Code == "InvalidKeyPair.Duplicate" {
		if s.YesNoPrompt(fmt.Sprintf("Key pair %s already exists, would you like to delete it?", keypairName)) {
			s.SendEvent("Deleting key pair")
			if err := s.ec2.DeleteKeyPair(&ec2.DeleteKeyPairRequest{
				KeyName: aws.String(keypairName),
			}); err != nil {
				return err
			}
			return s.createKeyPair()
		} else {
			for {
				keypairName = s.PromptInput("Please enter a new key pair name")
				if keypairName != "" {
					s.SSHKeyName = keypairName
					return s.createKeyPair()
				}
			}
		}
	}
	if err != nil {
		return err
	}

	s.SSHKey = keypair
	s.SSHKeyName = *res.KeyName

	err = saveSSHKey(keypairName, keypair)
	if err != nil {
		return err
	}
	return nil
}

type stackTemplateData struct {
	Instances           []struct{}
	DefaultInstanceType string
}

func (s *Stack) createStack() error {
	s.SendEvent("Generating start script")
	startScript, discoveryToken, err := genStartScript(s.NumInstances)
	if err != nil {
		return err
	}
	s.DiscoveryToken = discoveryToken
	s.persist()

	var stackTemplateBuffer bytes.Buffer
	err = stackTemplate.Execute(&stackTemplateBuffer, &stackTemplateData{
		Instances:           make([]struct{}, s.NumInstances),
		DefaultInstanceType: DefaultInstanceType,
	})
	if err != nil {
		return err
	}
	stackTemplateString := stackTemplateBuffer.String()

	parameters := []cloudformation.Parameter{
		{
			ParameterKey:   aws.String("ImageId"),
			ParameterValue: aws.String(s.ImageID),
		},
		{
			ParameterKey:   aws.String("ClusterDomain"),
			ParameterValue: aws.String(s.Domain.Name),
		},
		{
			ParameterKey:   aws.String("KeyName"),
			ParameterValue: aws.String(s.SSHKeyName),
		},
		{
			ParameterKey:   aws.String("UserData"),
			ParameterValue: aws.String(startScript),
		},
		{
			ParameterKey:   aws.String("InstanceType"),
			ParameterValue: aws.String(s.InstanceType),
		},
		{
			ParameterKey:   aws.String("VpcCidrBlock"),
			ParameterValue: aws.String(s.VpcCidr),
		},
		{
			ParameterKey:   aws.String("SubnetCidrBlock"),
			ParameterValue: aws.String(s.SubnetCidr),
		},
	}

	stackEventsSince := time.Now()

	if s.StackID != "" && s.StackName != "" {
		if err := s.fetchStack(); err == nil && !strings.HasPrefix(*s.Stack.StackStatus, "DELETE") {
			if s.YesNoPrompt(fmt.Sprintf("Stack found from previous installation (%s), would you like to delete it? (a new one will be created either way)", s.StackName)) {
				s.SendEvent(fmt.Sprintf("Deleting stack %s", s.StackName))
				if err := s.cf.DeleteStack(&cloudformation.DeleteStackInput{
					StackName: aws.String(s.StackName),
				}); err != nil {
					s.SendEvent(fmt.Sprintf("Unable to delete stack %s: %s", s.StackName, err))
				}
			}
		}
	}

	s.SendEvent("Creating stack")
	s.StackName = fmt.Sprintf("flynn-%d", time.Now().Unix())
	res, err := s.cf.CreateStack(&cloudformation.CreateStackInput{
		OnFailure:        aws.String("DELETE"),
		StackName:        aws.String(s.StackName),
		Tags:             []cloudformation.Tag{},
		TemplateBody:     aws.String(stackTemplateString),
		TimeoutInMinutes: aws.Integer(10),
		Parameters:       parameters,
	})
	if err != nil {
		return err
	}
	s.StackID = *res.StackID

	s.persist()
	return s.waitForStackCompletion("CREATE", stackEventsSince)
}

func fetchLatestVersion() (*release.EC2Version, error) {
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
	return manifest.Versions[0], nil
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

func (s *Stack) waitForStackCompletion(action string, after time.Time) error {
	stackID := aws.String(s.StackID)

	actionCompleteSuffix := "_COMPLETE"
	actionFailureSuffix := "_FAILED"
	isComplete := false
	isFailed := false

	stackEvents := make([]cloudformation.StackEvent, 0)
	var nextToken aws.StringValue

	var fetchStackEvents func() error
	fetchStackEvents = func() error {
		res, err := s.cf.DescribeStackEvents(&cloudformation.DescribeStackEventsInput{
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
				s.SendEvent(fmt.Sprintf("%s\t%s%s", name, *se.ResourceStatus, desc))
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
			return fmt.Errorf("Failed to create stack %s", s.StackName)
		}
		time.Sleep(1 * time.Second)
	}

	return nil
}

func (s *Stack) fetchStack() error {
	stackID := aws.String(s.StackID)

	s.SendEvent("Fetching stack")
	res, err := s.cf.DescribeStacks(&cloudformation.DescribeStacksInput{
		StackName: stackID,
	})
	if err != nil {
		return err
	}
	if len(res.Stacks) == 0 {
		return errors.New("Stack does not exist")
	}
	stack := &res.Stacks[0]
	if strings.HasPrefix(*stack.StackStatus, "DELETE_") {
		return fmt.Errorf("Stack in unusable state: %s", *stack.StackStatus)
	}
	s.Stack = stack
	return nil
}

func (s *Stack) fetchStackOutputs() error {
	s.fetchStack()

	s.InstanceIPs = make([]string, 0, s.NumInstances)
	for _, o := range s.Stack.Outputs {
		v := *o.OutputValue
		if strings.HasPrefix(*o.OutputKey, "IPAddress") {
			s.InstanceIPs = append(s.InstanceIPs, v)
		}
		if *o.OutputKey == "DNSZoneID" {
			s.DNSZoneID = v
		}
	}
	if len(s.InstanceIPs) != s.NumInstances {
		return fmt.Errorf("expected stack outputs to include %d instance IPs but found %d", s.NumInstances, len(s.InstanceIPs))
	}
	if s.DNSZoneID == "" {
		return fmt.Errorf("stack outputs do not include DNSZoneID")
	}

	return nil
}

func (s *Stack) configureDNS() error {
	// TODO(jvatic): Run directly after receiving zone create complete stack event
	s.SendEvent("Configuring DNS")

	// Set region to us-east-1, since any other region will fail for global services like Route53
	r53 := route53.New(s.Creds, "us-east-1", nil)
	res, err := r53.GetHostedZone(&route53.GetHostedZoneRequest{ID: aws.String(s.DNSZoneID)})
	if err != nil {
		return err
	}
	if err := s.Domain.Configure(res.DelegationSet.NameServers); err != nil {
		return err
	}

	return nil
}

func (s *Stack) waitForDNS() error {
	s.SendEvent("Waiting for DNS to propagate")
	for {
		status, err := s.Domain.Status()
		if err != nil {
			return err
		}
		if status == "applied" {
			break
		}
		time.Sleep(time.Second)
	}
	s.SendEvent("DNS is live")
	return nil
}

func instanceRunCmd(cmd string, sshConfig *ssh.ClientConfig, ipAddress string) (stdout, stderr io.Reader, err error) {
	var sshConn *ssh.Client
	sshConn, err = ssh.Dial("tcp", ipAddress+":22", sshConfig)
	if err != nil {
		return
	}
	defer sshConn.Close()

	sess, err := sshConn.NewSession()
	if err != nil {
		return
	}
	stdout, err = sess.StdoutPipe()
	if err != nil {
		return
	}
	stderr, err = sess.StderrPipe()
	if err != nil {
		return
	}
	if err = sess.Start(cmd); err != nil {
		return
	}

	err = sess.Wait()
	return
}

func (s *Stack) uploadDebugInfo(sshConfig *ssh.ClientConfig, ipAddress string) {
	cmd := "sudo flynn-host upload-debug-info"
	stdout, stderr, _ := instanceRunCmd(cmd, sshConfig, ipAddress)
	var buf bytes.Buffer
	io.Copy(&buf, stdout)
	io.Copy(&buf, stderr)
	s.SendEvent(fmt.Sprintf("`%s` output for %s: %s", cmd, ipAddress, buf.String()))
}

type stepInfo struct {
	ID        string           `json:"id"`
	Action    string           `json:"action"`
	Data      *json.RawMessage `json:"data"`
	State     string           `json:"state"`
	Error     string           `json:"error,omitempty"`
	Timestamp time.Time        `json:"ts"`
}

func (s *Stack) bootstrap() error {
	s.SendEvent("Running bootstrap")

	if s.Stack == nil {
		return errors.New("No stack found")
	}

	if s.SSHKey == nil {
		return errors.New("No SSHKey found")
	}

	// bootstrap only needs to run on one instance
	ipAddress := s.InstanceIPs[0]

	signer, err := ssh.NewSignerFromKey(s.SSHKey.PrivateKey)
	if err != nil {
		return err
	}
	sshConfig := &ssh.ClientConfig{
		User: "ubuntu",
		Auth: []ssh.AuthMethod{ssh.PublicKeys(signer)},
	}

	attempts := 0
	maxAttempts := 3
	var sshConn *ssh.Client
	for {
		sshConn, err = ssh.Dial("tcp", ipAddress+":22", sshConfig)
		if err != nil {
			if attempts < maxAttempts {
				attempts += 1
				time.Sleep(time.Second)
				continue
			}
			return err
		}
		break
	}
	defer sshConn.Close()

	sess, err := sshConn.NewSession()
	if err != nil {
		return err
	}
	stdout, err := sess.StdoutPipe()
	if err != nil {
		return err
	}
	sess.Stderr = os.Stderr
	if err := sess.Start(fmt.Sprintf("CLUSTER_DOMAIN=%s flynn-host bootstrap --json", s.Domain.Name)); err != nil {
		s.uploadDebugInfo(sshConfig, ipAddress)
		return err
	}

	var keyData struct {
		Key string `json:"data"`
	}
	var loginTokenData struct {
		Token string `json:"data"`
	}
	var controllerCertData struct {
		Pin    string `json:"pin"`
		CACert string `json:"ca_cert"`
	}
	output := json.NewDecoder(stdout)
	for {
		var stepRaw json.RawMessage
		if err := output.Decode(&stepRaw); err != nil {
			if err == io.EOF {
				break
			}
			return err
		}
		var step stepInfo
		if err := json.Unmarshal(stepRaw, &step); err != nil {
			return err
		}
		if step.State == "error" {
			s.uploadDebugInfo(sshConfig, ipAddress)
			return fmt.Errorf("bootstrap: %s %s error: %s", step.ID, step.Action, step.Error)
		}
		s.SendEvent(fmt.Sprintf("%s: %s", step.ID, step.State))
		if step.State != "done" {
			continue
		}
		switch step.ID {
		case "controller-key":
			if err := json.Unmarshal(*step.Data, &keyData); err != nil {
				return err
			}
		case "controller-cert":
			if err := json.Unmarshal(*step.Data, &controllerCertData); err != nil {
				return err
			}
		case "dashboard-login-token":
			if err := json.Unmarshal(*step.Data, &loginTokenData); err != nil {
				return err
			}
		case "log-complete":
			break
		}
	}
	if keyData.Key == "" || controllerCertData.Pin == "" {
		return err
	}

	s.ControllerKey = keyData.Key
	s.ControllerPin = controllerCertData.Pin
	s.CACert = controllerCertData.CACert
	s.DashboardLoginToken = loginTokenData.Token

	if err := sess.Wait(); err != nil {
		return err
	}
	if err := s.waitForDNS(); err != nil {
		return err
	}

	return nil
}

func genStartScript(nodes int) (string, string, error) {
	var data struct {
		DiscoveryToken string
	}
	var err error
	data.DiscoveryToken, err = etcdcluster.NewDiscoveryToken(strconv.Itoa(nodes))
	if err != nil {
		return "", "", err
	}
	buf := &bytes.Buffer{}
	w := base64.NewEncoder(base64.StdEncoding, buf)
	err = startScript.Execute(w, data)
	w.Close()

	return buf.String(), data.DiscoveryToken, err
}

var startScript = template.Must(template.New("start.sh").Parse(`
#!/bin/sh

# wait for libvirt
while ! [ -e /var/run/libvirt/libvirt-sock ]; do
  sleep 0.1
done

flynn-host init --discovery={{.DiscoveryToken}}
start flynn-host
`[1:]))

func (s *Stack) GetOutput(name string) (string, error) {
	var value string
	for _, o := range s.Stack.Outputs {
		if *o.OutputKey == name {
			value = *o.OutputValue
			break
		}
	}
	if value == "" {
		return "", fmt.Errorf("stack outputs do not include %s", name)
	}
	return value, nil
}
