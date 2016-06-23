package awscluster

import (
	"fmt"

	"github.com/flynn/flynn/pkg/installer"
	"github.com/flynn/flynn/pkg/installer/common"
	"github.com/flynn/flynn/pkg/sshkeygen"
)

func init() {
	installer.Register("aws", &Cluster{})
}

// Cluster manages Flynn clusters on AWS
type Cluster struct {
	common.BaseCluster

	// inputs
	InstanceType string    `json:"instance_type"`
	VpcCIDR      string    `json:"vpc_cidr,omitempty"`
	SubnetCIDR   string    `json:"subnet_cidr,omitempty"`
	AWSClient    AWSClient `json:"-"`

	sshKey  *installer.SSHKey
	imageID string
	stackID string
}

// LaunchSteps returns steps for launching a Flynn cluster on AWS
// and is used by installer.LaunchCluster
func (c *Cluster) LaunchSteps() []installer.Step {
	return []installer.Step{
		{"Validating input", c.validateInputStep},
		{"Resolving SSH key", c.resolveSSHKeyStep},
		{"Allocating domain", c.allocateDomainStep},
		{"Resolving image ID", c.resolveImageIDStep},
		{"Generating start script", c.generateStartScriptStep},
		{"Creating stack", c.createStackStep},
		{"Fetching stack outputs", c.fetchStackOutputsStep},
		{"Configuring DNS", c.configureDNSStep},
		{"Uploading backup", c.uploadBackupStep},
		// TODO(jvatic): Start bootstrapping as soon as instances are up (https://github.com/flynn/flynn/issues/1113)
		{"Running bootstrap", c.bootstrapStep},
		{"Waiting for DNS to propagate", c.waitForDNSStep},
	}
}

func (c *Cluster) validateInputStep(ctx installer.EventContext) error {
	// TODO
	return nil
}

func (c *Cluster) resolveSSHKeyStep(ctx installer.EventContext) error {
	keyPairs := ctx.SSHKeysPrompt()
	for _, k := range keyPairs {
		if name, err := c.AWSClient.CheckKeyPairExists(k); err == nil {
			k = &installer.SSHKey{
				Name:       name,
				PublicKey:  k.PublicKey,
				PrivateKey: k.PrivateKey,
			}
			c.sshKey = k
			ctx.Log(installer.LogLevelInfo, fmt.Sprintf("Using saved key pair (%s)", name))
			ctx.SendOutput("ssh_key", k)
			return nil
		}
	}

	name := installer.DEFAULT_SSH_KEY_NAME
	for _, k := range keyPairs {
		if k.Name == name {
			ctx.Log(installer.LogLevelInfo, fmt.Sprintf("Importing key pair (%s)", name))
			c.sshKey = k
			if err := c.AWSClient.ImportKeyPair(ctx, k); err != nil {
				return err
			}
			ctx.SendOutput("ssh_key", k)
			return nil
		}
	}

	ctx.Log(installer.LogLevelInfo, fmt.Sprintf("Creating key pair (%s)", name))
	key, err := sshkeygen.Generate()
	if err != nil {
		return err
	}
	c.sshKey = &installer.SSHKey{
		Name:       name,
		PublicKey:  key.PublicKey,
		PrivateKey: key.PrivateKey,
	}
	if err := c.AWSClient.ImportKeyPair(ctx, c.sshKey); err != nil {
		return err
	}
	ctx.SendOutput("ssh_key", c.sshKey)
	return nil
}

func (c *Cluster) allocateDomainStep(ctx installer.EventContext) error {
	// TODO
	return nil
}

func (c *Cluster) resolveImageIDStep(ctx installer.EventContext) error {
	// TODO
	return nil
}

func (c *Cluster) generateStartScriptStep(ctx installer.EventContext) error {
	// TODO
	return nil
}

func (c *Cluster) createStackStep(ctx installer.EventContext) error {
	// TODO
	return nil
}

func (c *Cluster) fetchStackOutputsStep(ctx installer.EventContext) error {
	// TODO
	return nil
}

func (c *Cluster) configureDNSStep(ctx installer.EventContext) error {
	// TODO
	return nil
}

func (c *Cluster) uploadBackupStep(ctx installer.EventContext) error {
	// TODO
	return nil
}

func (c *Cluster) bootstrapStep(ctx installer.EventContext) error {
	// TODO
	return nil
}

func (c *Cluster) waitForDNSStep(ctx installer.EventContext) error {
	// TODO
	return nil
}

// DestroySteps returns steps for destroying a Flynn cluster on AWS
// and is used by installer.DestroyCluster
func (c *Cluster) DestroySteps() []installer.Step {
	// TODO
	return []installer.Step{}
}
