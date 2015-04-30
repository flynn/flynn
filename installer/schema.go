package installer

import (
	"fmt"
	"time"

	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/awslabs/aws-sdk-go/aws"
	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/awslabs/aws-sdk-go/gen/cloudformation"
	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/awslabs/aws-sdk-go/gen/ec2"
	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/cznic/ql"
	"github.com/flynn/flynn/pkg/sshkeygen"
)

type Cluster interface {
	Base() *BaseCluster
}

type credential struct {
	ID     string `json:"id" ql:"index xID"`
	Secret string `json:"secret"`
}

type AWSCluster struct {
	ClusterID    string     `json:"cluster_id" ql:"index xCluster"`
	StackID      string     `json:"stack_id"`
	StackName    string     `json:"stack_name"`
	ImageID      string     `json:"image_id,omitempty"`
	Region       string     `json:"region"`
	InstanceType string     `json:"instance_type"`
	VpcCIDR      string     `json:"vpc_cidr"`
	SubnetCIDR   string     `json:"subnet_cidr"`
	DNSZoneID    string     `json:"dns_zone_id"`
	DeletedAt    *time.Time `json:"deleted_at,omitempty"`

	base  *BaseCluster
	creds aws.CredentialsProvider
	stack *cloudformation.Stack
	cf    *cloudformation.CloudFormation
	ec2   *ec2.EC2
}

type BaseCluster struct {
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
	VpcCIDR             string            `json:"vpc_cidr_block,omitempty"`
	SubnetCIDR          string            `json:"subnet_cidr_block,omitempty"`
	DiscoveryToken      string            `json:"discovery_token"`
	InstanceIPs         []string          `json:"instance_ips,omitempty" ql:"-"`
	DNSZoneID           string            `json:"dns_zone_id,omitempty"`
	DeletedAt           *time.Time        `json:"deleted_at,omitempty"`

	installer     *Installer
	pendingPrompt *Prompt
	done          bool
}

type InstanceIPs struct {
	ClusterID string `ql:"index xCluster"`
	IP        string
	DeletedAt *time.Time `json:"deleted_at,omitempty"`
}

type Event struct {
	ID          string       `json:"id" ql:"index xID"`
	Timestamp   time.Time    `json:"timestamp"`
	Type        string       `json:"type"`
	ClusterID   string       `json:"cluster_id",omitempty`
	PromptID    string       `json:"-"`
	Description string       `json:"description,omitempty"`
	Prompt      *Prompt      `json:"prompt,omitempty" ql:"-"`
	Cluster     *BaseCluster `json:"cluster,omitempty" ql:"-"`
	DeletedAt   *time.Time   `json:"deleted_at,omitempty"`
}

type Prompt struct {
	ID        string     `json:"id"`
	Type      string     `json:"type,omitempty"`
	Message   string     `json:"message,omitempty"`
	Yes       bool       `json:"yes,omitempty"`
	Input     string     `json:"input,omitempty"`
	Resolved  bool       `json:"resolved,omitempty"`
	DeletedAt *time.Time `json:"deleted_at,omitempty"`
	resChan   chan *Prompt
	cluster   *BaseCluster
}

func (i *Installer) updatedbColumns(in interface{}, t string) error {
	s, err := ql.StructSchema(in)
	if err != nil {
		return err
	}
	rows, err := i.db.Query(fmt.Sprintf("SELECT * FROM %s LIMIT 0", t))
	if err != nil {
		return err
	}
	defer rows.Close()

	var add []string
	var remove []string

	dbColumns, err := rows.Columns()
	if err != nil {
		return err
	}

	fields := make(map[string]ql.Type, len(s.Fields))
	for _, f := range s.Fields {
		fields[f.Name] = f.Type
	}

	dbFieldMap := make(map[string]bool, len(dbColumns))
	for _, c := range dbColumns {
		if _, ok := fields[c]; !ok {
			remove = append(remove, c)
			continue
		}
		dbFieldMap[c] = true
	}

	for c := range fields {
		if _, ok := dbFieldMap[c]; !ok {
			add = append(add, c)
		}
	}

	tx, err := i.db.Begin()
	if err != nil {
		return err
	}

	for _, c := range remove {
		if _, err := tx.Exec(fmt.Sprintf(`
      ALTER TABLE %s DROP %s
    `, t, c)); err != nil {
			tx.Rollback()
			return err
		}
	}

	for _, c := range add {
		if _, err := tx.Exec(fmt.Sprintf(`
      ALTER TABLE %s ADD %s %s
    `, t, c, fields[c])); err != nil {
			tx.Rollback()
			return err
		}
	}

	return tx.Commit()
}

func (i *Installer) migrateDB() error {
	schemaInterfaces := map[interface{}]string{
		(*credential)(nil):  "credentials",
		(*BaseCluster)(nil): "clusters",
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
	if err := tx.Commit(); err != nil {
		return err
	}

	for item, tableName := range schemaInterfaces {
		if err := i.updatedbColumns(item, tableName); err != nil {
			return err
		}
	}
	return nil
}
