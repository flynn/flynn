package installer

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/awslabs/aws-sdk-go/aws"
	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/cznic/ql"
	log "github.com/flynn/flynn/Godeps/_workspace/src/gopkg.in/inconshreveable/log15.v2"
)

var ClusterNotFoundError = errors.New("Cluster not found")

type Installer struct {
	db            *sql.DB
	events        []*Event
	subscriptions []*Subscription
	clusters      []Cluster
	logger        log.Logger

	dbMtx        sync.RWMutex
	eventsMtx    sync.Mutex
	subscribeMtx sync.Mutex
	clustersMtx  sync.RWMutex
}

func NewInstaller(l log.Logger) *Installer {
	installer := &Installer{
		events:        make([]*Event, 0),
		subscriptions: make([]*Subscription, 0),
		clusters:      make([]Cluster, 0),
		logger:        l,
	}
	if err := installer.openDB(); err != nil {
		panic(err)
	}
	return installer
}

func (i *Installer) txExec(query string, args ...interface{}) error {
	tx, err := i.db.Begin()
	if err != nil {
		return err
	}
	_, err = tx.Exec(query, args...)
	if err != nil {
		tx.Rollback()
		return err
	}
	return tx.Commit()
}

func (i *Installer) LaunchCluster(c interface{}) error {
	switch v := c.(type) {
	case *AWSCluster:
		return i.launchAWSCluster(v)
	default:
		return fmt.Errorf("Invalid cluster type %T", c)
	}
}

func (i *Installer) launchAWSCluster(c *AWSCluster) error {
	if err := c.SetDefaultsAndValidate(); err != nil {
		return err
	}

	if err := i.saveAWSCluster(c); err != nil {
		return err
	}

	i.clustersMtx.Lock()
	i.clusters = append(i.clusters, c)
	i.clustersMtx.Unlock()
	i.SendEvent(&Event{
		Type:      "new_cluster",
		Cluster:   c.base,
		ClusterID: c.base.ID,
	})
	c.Run()
	return nil
}

func (i *Installer) saveAWSCluster(c *AWSCluster) error {
	i.dbMtx.Lock()
	defer i.dbMtx.Unlock()

	c.ClusterID = c.base.ID
	c.base.Type = "aws"
	c.base.Name = c.ClusterID

	clusterFields, err := ql.Marshal(c.base)
	if err != nil {
		return err
	}
	clustersVStr := make([]string, 0, len(clusterFields))
	for idx := range clusterFields {
		clustersVStr = append(clustersVStr, fmt.Sprintf("$%d", idx+1))
	}

	awsFields, err := ql.Marshal(c)
	if err != nil {
		return err
	}
	awsVStr := make([]string, 0, len(awsFields))
	for idx := range awsFields {
		awsVStr = append(awsVStr, fmt.Sprintf("$%d", idx+1))
	}

	if err != nil {
		return err
	}
	tx, err := i.db.Begin()
	if err != nil {
		return err
	}
	if _, err := tx.Exec(fmt.Sprintf("INSERT INTO clusters VALUES (%s)", strings.Join(clustersVStr, ",")), clusterFields...); err != nil {
		tx.Rollback()
		return err
	}
	if _, err := tx.Exec(fmt.Sprintf("INSERT INTO aws_clusters VALUES (%s)", strings.Join(awsVStr, ",")), awsFields...); err != nil {
		tx.Rollback()
		return err
	}
	return tx.Commit()
}

func (i *Installer) SaveAWSCredentials(id, secret string) error {
	i.dbMtx.Lock()
	defer i.dbMtx.Unlock()
	return i.txExec(`
		INSERT INTO credentials (ID, Secret) VALUES ($1, $2);
  `, id, secret)
}

func (i *Installer) FindAWSCredentials(id string) (aws.CredentialsProvider, error) {
	if id == "aws_env" {
		return aws.EnvCreds()
	}
	var secret string

	if err := i.db.QueryRow(`SELECT Secret FROM credentials WHERE id == $1 LIMIT 1`, id).Scan(&secret); err != nil {
		return nil, err
	}
	return aws.Creds(id, secret, ""), nil
}

func (i *Installer) FindCluster(id string) (*BaseCluster, error) {
	i.clustersMtx.RLock()
	for _, c := range i.clusters {
		if cluster, ok := c.(*AWSCluster); ok {
			if cluster.ClusterID == id {
				i.clustersMtx.RUnlock()
				return cluster.base, nil
			}
		}
	}
	i.clustersMtx.RUnlock()

	c := &BaseCluster{ID: id, installer: i}

	err := i.db.QueryRow(`
	SELECT CredentialID, Type, State, NumInstances, ControllerKey, ControllerPin, DashboardLoginToken, CACert, SSHKeyName, VpcCIDR, SubnetCIDR, DiscoveryToken, DNSZoneID FROM clusters WHERE ID == $1 AND DeletedAt IS NULL LIMIT 1
  `, c.ID).Scan(&c.CredentialID, &c.Type, &c.State, &c.NumInstances, &c.ControllerKey, &c.ControllerPin, &c.DashboardLoginToken, &c.CACert, &c.SSHKeyName, &c.VpcCIDR, &c.SubnetCIDR, &c.DiscoveryToken, &c.DNSZoneID)
	if err != nil {
		return nil, err
	}

	domain := &Domain{ClusterID: c.ID}
	err = i.db.QueryRow(`
  SELECT Name, Token FROM domains WHERE ClusterID == $1 AND DeletedAt IS NULL LIMIT 1
  `, c.ID).Scan(&domain.Name, &domain.Token)
	if err != nil && err != sql.ErrNoRows {
		return nil, err
	}
	if err == nil {
		c.Domain = domain
	}

	var instanceIPs []string
	rows, err := i.db.Query(`SELECT IP FROM instances WHERE ClusterID == $1 AND DeletedAt IS NULL`, c.ID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var ip string
		err = rows.Scan(&ip)
		if err != nil {
			return nil, err
		}
	}
	c.InstanceIPs = instanceIPs

	return c, nil
}

func (i *Installer) FindAWSCluster(id string) (*AWSCluster, error) {
	i.clustersMtx.RLock()
	for _, c := range i.clusters {
		if cluster, ok := c.(*AWSCluster); ok {
			if cluster.ClusterID == id {
				i.clustersMtx.RUnlock()
				return cluster, nil
			}
		}
	}
	i.clustersMtx.RUnlock()

	cluster, err := i.FindCluster(id)
	if err != nil {
		return nil, err
	}

	awsCluster := &AWSCluster{
		base: cluster,
	}

	err = i.db.QueryRow(`
	SELECT StackID, StackName, ImageID, Region, InstanceType, VpcCIDR, SubnetCIDR, DNSZoneID FROM aws_clusters WHERE ClusterID == $1 AND DeletedAt IS NULL LIMIT 1
  `, cluster.ID).Scan(&awsCluster.StackID, &awsCluster.StackName, &awsCluster.ImageID, &awsCluster.Region, &awsCluster.InstanceType, &awsCluster.VpcCIDR, &awsCluster.SubnetCIDR, &awsCluster.DNSZoneID)
	if err != nil {
		return nil, err
	}

	creds, err := i.FindAWSCredentials(cluster.CredentialID)
	if err != nil {
		return nil, err
	}
	awsCluster.creds = creds

	return awsCluster, nil
}

func (i *Installer) DeleteCluster(id string) error {
	awsCluster, err := i.FindAWSCluster(id)
	if err != nil {
		return err
	}
	go awsCluster.Delete()
	return nil
}

func (i *Installer) ClusterDeleted(id string) {
	i.clustersMtx.Lock()
	defer i.clustersMtx.Unlock()
	clusters := make([]Cluster, 0, len(i.clusters))
	for _, c := range i.clusters {
		if c.Base().ID != id {
			clusters = append(clusters, c)
		}
	}
	i.clusters = clusters
}
