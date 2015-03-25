package installer

import (
	"time"

	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/awslabs/aws-sdk-go/aws"
	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/awslabs/aws-sdk-go/gen/cloudformation"
	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/awslabs/aws-sdk-go/gen/ec2"
	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/cznic/ql"
	"github.com/flynn/flynn/pkg/sshkeygen"
)

type credential struct {
	ID     string `json:"id" ql:"index xID"`
	Secret string `json:"secret"`
}

type AWSCluster struct {
	ClusterID    string `json:"cluster_id" ql:"index xCluster"`
	StackID      string `json:"stack_id"`
	StackName    string `json:"stack_name"`
	ImageID      string `json:"image_id,omitempty"`
	Region       string `json:"region"`
	InstanceType string `json:"instance_type"`
	VpcCidr      string `json:"vpc_cidr"`
	SubnetCidr   string `json:"subnet_cidr"`
	DNSZoneID    string `json:"dns_zone_id"`

	cluster *Cluster
	creds   aws.CredentialsProvider
	stack   *cloudformation.Stack
	cf      *cloudformation.CloudFormation
	ec2     *ec2.EC2
}

type Cluster struct {
	ID                  string            `json:"id" ql:"index xID"`
	CredentialID        string            `json:"-"`
	Type                string            `json:"type"`                    // enum(aws)
	State               string            `json:"state" ql:"index xState"` // enum(starting, error, running, deleting)
	Name                string            `json:"name" ql:"-"`
	NumInstances        int64             `json:"num_instances"`
	ControllerKey       string            `json:"controller_key,omitempty"`
	ControllerPin       string            `json:"controller_pin,omitempty"`
	DashboardLoginToken string            `json:"dashboard_login_token,omitempty"`
	Domain              *Domain           `json:"domain" ql:"-"`
	CACert              string            `json:"ca_cert"`
	SSHKey              *sshkeygen.SSHKey `json:"-" ql:"-"`
	SSHKeyName          string            `json:"ssh_key_name,omitempty"`
	VpcCidr             string            `json:"vpc_cidr_block,omitempty"`
	SubnetCidr          string            `json:"subnet_cidr_block,omitempty"`
	DiscoveryToken      string            `json:"discovery_token"`
	InstanceIPs         []string          `json:"instance_ips,omitempty" ql:"-"`
	DNSZoneID           string            `json:"dns_zone_id,omitempty"`

	installer     *Installer
	pendingPrompt *Prompt
	done          bool
}

type InstanceIPs struct {
	ClusterID string `ql:"index xCluster"`
	IP        string
}

type Event struct {
	ID          string    `json:"id" ql:"index xID"`
	Timestamp   time.Time `json:"timestamp"`
	Type        string    `json:"type"`
	ClusterID   string    `json:"cluster_id",omitempty`
	PromptID    string    `json:"-"`
	Description string    `json:"description,omitempty"`
	Prompt      *Prompt   `json:"prompt,omitempty" ql:"-"`
	Cluster     *Cluster  `json:"cluster,omitempty" ql:"-"`
}

type Prompt struct {
	ID       string `json:"id"`
	Type     string `json:"type,omitempty"`
	Message  string `json:"message,omitempty"`
	Yes      bool   `json:"yes,omitempty"`
	Input    string `json:"input,omitempty"`
	Resolved bool   `json:"resolved,omitempty"`
	resChan  chan *Prompt
	cluster  *Cluster
}

func (i *Installer) migrateDB() error {
	schemaInterfaces := map[interface{}]string{
		(*credential)(nil):  "credentials",
		(*Cluster)(nil):     "clusters",
		(*AWSCluster)(nil):  "aws_clusters",
		(*Event)(nil):       "events",
		(*Prompt)(nil):      "prompts",
		(*InstanceIPs)(nil): "instances",
		(*Domain)(nil):      "domains",
	}

	tx, err := i.db.Begin()
	if err != nil {
		return err
	}
	for item, tableName := range schemaInterfaces {
		schema, err := ql.Schema(item, tableName, nil)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(schema.String()); err != nil {
			tx.Rollback()
			return err
		}
	}
	return tx.Commit()
}
