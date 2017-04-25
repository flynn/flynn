package installer

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"sync"

	"github.com/digitalocean/godo"
	"github.com/flynn/flynn/pkg/httphelper"
	"github.com/flynn/flynn/pkg/shutdown"
	"github.com/flynn/flynn/util/release/types"
	log "gopkg.in/inconshreveable/log15.v2"
)

var ClusterNotFoundError = errors.New("Cluster not found")

type ReleaseChannel struct {
	Name    string                   `json:"name"`
	Version string                   `json:"version"`
	History []*ReleaseChannelVersion `json:"history"`
}

type ReleaseChannelVersion struct {
	Version string `json:"version"`
}

type Installer struct {
	db              *sql.DB
	events          []*Event
	subscriptions   []*Subscription
	clusters        []Cluster
	logger          log.Logger
	releaseChannels []*ReleaseChannel
	ec2Versions     []*release.EC2Version

	dbMtx        sync.RWMutex
	eventsMtx    sync.RWMutex
	subscribeMtx sync.Mutex
	clustersMtx  sync.RWMutex
}

func NewInstaller(l log.Logger) *Installer {
	installer := &Installer{
		subscriptions: make([]*Subscription, 0),
		clusters:      make([]Cluster, 0),
		logger:        l,
	}
	if err := installer.openDB(); err != nil {
		if err.Error() == "resource temporarily unavailable" {
			shutdown.Fatal("Error: Another `flynn install` process is already running.")
		}
		shutdown.Fatalf("Error opening database: %s", err)
	}
	if err := installer.loadEventsFromDB(); err != nil {
		shutdown.Fatalf("Error loading events from database: %s", err)
	}

	// Get current list of releases
	resp, err := http.Get("https://releases.flynn.io/api/channels")
	if err != nil {
		l.Debug(fmt.Sprintf("Unable to fetch releases: %s", err))
		return installer
	}
	defer resp.Body.Close()
	var channels []*ReleaseChannel
	if err := json.NewDecoder(resp.Body).Decode(&channels); err != nil {
		l.Debug(fmt.Sprintf("Unable to read list of fetched releases: %s", err))
		return installer
	}
	installer.releaseChannels = channels

	// Get current list of EC2 images
	ec2Versions, err := installer.fetchEC2Versions()
	if err != nil {
		l.Debug(fmt.Sprintf("Unable to fetch EC2 images: %s", err))
		return installer
	}
	installer.ec2Versions = ec2Versions

	return installer
}

func (i *Installer) loadEventsFromDB() error {
	var events []*Event
	rows, err := i.db.Query(`
    SELECT ID, Timestamp, Type, ClusterID, ResourceType, ResourceID, Description FROM events WHERE DeletedAt IS NULL ORDER BY Timestamp
  `)
	if err != nil {
		return err
	}
	for rows.Next() {
		e := &Event{}
		if err := rows.Scan(&e.ID, &e.Timestamp, &e.Type, &e.ClusterID, &e.ResourceType, &e.ResourceID, &e.Description); err != nil {
			return err
		}
		events = append(events, e)
	}
	i.events = events
	return nil
}

func (i *Installer) fetchEC2Versions() ([]*release.EC2Version, error) {
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
	return manifest.Versions, nil
}

func (i *Installer) removeClusterEvents(clusterID string) {
	i.eventsMtx.Lock()
	defer i.eventsMtx.Unlock()
	events := make([]*Event, 0, len(i.events))
	for _, e := range i.events {
		if e.ClusterID == "" || e.ClusterID != clusterID {
			events = append(events, e)
		}
	}
	i.events = events
}

func (i *Installer) removeClusterLogEvents(clusterID string) {
	i.eventsMtx.Lock()
	defer i.eventsMtx.Unlock()
	events := make([]*Event, 0, len(i.events))
	for _, e := range i.events {
		if e.Type != "log" || e.ClusterID != clusterID {
			events = append(events, e)
		}
	}
	i.events = events
}

func (i *Installer) removeCredentialEvents(credID string) {
	i.eventsMtx.Lock()
	defer i.eventsMtx.Unlock()
	events := make([]*Event, 0, len(i.events))
	for _, e := range i.events {
		if e.ResourceType != "credential" || e.ResourceID != credID {
			events = append(events, e)
		}
	}
	i.events = events
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

var credentialExistsError = errors.New("Credential already exists")

func (i *Installer) SaveCredentials(creds *Credential) error {
	i.dbMtx.Lock()
	defer i.dbMtx.Unlock()
	if _, err := i.FindCredentials(creds.ID); err == nil {
		return credentialExistsError
	}
	tx, err := i.db.Begin()
	if err != nil {
		return err
	}
	if _, err := tx.Exec(`
		INSERT INTO credentials (ID, Secret, Name, Type, Endpoint) VALUES ($1, $2, $3, $4, $5);
  `, creds.ID, creds.Secret, creds.Name, creds.Type, creds.Endpoint); err != nil {
		if strings.Contains(err.Error(), "duplicate value") {
			if _, err := tx.Exec(`
				UPDATE credentials SET Secret = $2, Name = $3, Type = $4, Endpoint = $5, DeletedAt = NULL WHERE ID == $1 AND DeletedAt IS NOT NULL
			`, creds.ID, creds.Secret, creds.Name, creds.Type, creds.Endpoint); err != nil {
				tx.Rollback()
				return err
			}
			if _, err := tx.Exec(`UPDATE events SET DeletedAt = now() WHERE ResourceType == "credential" AND ResourceID == $1`, creds.ID); err != nil {
				tx.Rollback()
				return err
			}
			i.removeCredentialEvents(creds.ID)
		} else {
			tx.Rollback()
			return err
		}
	}
	if creds.Type == "azure" {
		for _, oc := range creds.OAuthCreds {
			if _, err := tx.Exec(`
				INSERT INTO oauth_credentials (ClientID, AccessToken, RefreshToken, ExpiresAt, Scope) VALUES ($1, $2, $3, $4, $5);
			`, oc.ClientID, oc.AccessToken, oc.RefreshToken, oc.ExpiresAt, oc.Scope); err != nil {
				tx.Rollback()
				return err
			}
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	go i.SendEvent(&Event{
		Type:         "new_credential",
		ResourceType: "credential",
		ResourceID:   creds.ID,
		Resource:     creds,
	})
	return nil
}

func (i *Installer) DeleteCredentials(id string) error {
	if _, err := i.FindCredentials(id); err != nil {
		return err
	}
	var count int64
	if err := i.db.QueryRow(`SELECT count() FROM clusters WHERE CredentialID == $1 AND DeletedAt IS NULL`, id).Scan(&count); err != nil {
		return err
	}
	if count > 0 {
		return httphelper.JSONError{
			Code:    httphelper.ConflictErrorCode,
			Message: "Credential is currently being used by one or more clusters",
		}
	}
	if err := i.txExec(`UPDATE credentials SET DeletedAt = now() WHERE ID == $1`, id); err != nil {
		return err
	}
	if err := i.txExec(`UPDATE oauth_credentials SET DeletedAt = now() WHERE ClientID == $1`, id); err != nil {
		return err
	}
	if err := i.txExec(`UPDATE events SET DeletedAt = now() WHERE ResourceType == "credential" AND ResourceID == $1`, id); err != nil {
		return err
	}
	i.removeCredentialEvents(id)
	go i.SendEvent(&Event{
		Type:         "delete_credential",
		ResourceType: "credential",
		ResourceID:   id,
	})
	return nil
}

func (i *Installer) FindCredentials(id string) (*Credential, error) {
	creds := &Credential{}
	var endpoint *string
	if err := i.db.QueryRow(`SELECT ID, Secret, Name, Type, Endpoint FROM credentials WHERE ID == $1 AND DeletedAt IS NULL LIMIT 1`, id).Scan(&creds.ID, &creds.Secret, &creds.Name, &creds.Type, &endpoint); err != nil {
		return nil, err
	}
	if endpoint != nil {
		creds.Endpoint = *endpoint
	}
	if creds.Type == "azure" {
		oauthCreds := make([]*OAuthCredential, 0, 2)
		rows, err := i.db.Query(`SELECT AccessToken, RefreshToken, ExpiresAt, Scope FROM oauth_credentials WHERE ClientID == $1`, creds.ID)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		for rows.Next() {
			oc := &OAuthCredential{ClientID: creds.ID}
			if err := rows.Scan(&oc.AccessToken, &oc.RefreshToken, &oc.ExpiresAt, &oc.Scope); err != nil {
				return nil, err
			}
			oauthCreds = append(oauthCreds, oc)
		}
		if err := rows.Err(); err != nil {
			return nil, err
		}
		creds.OAuthCreds = oauthCreds
	}
	return creds, nil
}

func (i *Installer) LaunchCluster(c Cluster) error {
	if err := c.SetDefaultsAndValidate(); err != nil {
		return err
	}

	if err := i.SaveCluster(c); err != nil {
		return err
	}

	base := c.Base()

	i.clustersMtx.Lock()
	i.clusters = append(i.clusters, c)
	i.clustersMtx.Unlock()
	i.SendEvent(&Event{
		Type:      "new_cluster",
		Cluster:   base,
		ClusterID: base.ID,
	})
	if base.IsRestoringBackup() {
		base.SendProgress(&ProgressEvent{
			ID:          "upload-backup",
			Description: "Upload pending cluster start...",
			Percent:     0,
		})
	}
	c.Run()
	return nil
}

func (i *Installer) ListDigitalOceanRegions(creds *Credential) (interface{}, error) {
	client := digitalOceanClient(creds)
	regions, r, err := client.Regions.List(&godo.ListOptions{})
	if err != nil {
		code := httphelper.UnknownErrorCode
		if r.StatusCode == 401 {
			code = httphelper.UnauthorizedErrorCode
		}
		return nil, httphelper.JSONError{
			Code:    code,
			Message: err.Error(),
		}
	}
	res := make([]godo.Region, 0, len(regions))
	for _, r := range regions {
		if r.Available {
			res = append(res, r)
		}
	}
	return res, err
}

func (i *Installer) ListAzureRegions(creds *Credential) (interface{}, error) {
	type azureLocation struct {
		Name string `json:"name"`
		Slug string `json:"slug"`
	}
	client := i.azureClient(creds)
	res, err := client.ListLocations("Microsoft.Compute", "virtualMachines")
	if err != nil {
		return nil, err
	}
	locs := make([]azureLocation, 0, len(res))
	for _, l := range res {
		locs = append(locs, azureLocation{
			Name: l,
			Slug: l,
		})
	}
	return locs, nil
}

func (i *Installer) dbMarshalItem(tableName string, item interface{}, typeMap map[string]interface{}) ([]interface{}, error) {
	if typeMap == nil {
		typeMap = make(map[string]interface{})
	}
	rows, err := i.db.Query(fmt.Sprintf("SELECT * FROM %s LIMIT 0", tableName))
	if err != nil {
		return nil, err
	}
	cols, err := rows.Columns()
	if err != nil {
		return nil, err
	}

	v := reflect.Indirect(reflect.ValueOf(item))
	fields := make([]interface{}, len(cols))
	for idx, c := range cols {
		fields[idx] = v.FieldByName(c).Interface()
		if t, ok := typeMap[c]; ok {
			fields[idx] = reflect.ValueOf(fields[idx]).Convert(reflect.TypeOf(t)).Interface()
		}
	}
	return fields, nil
}

func (i *Installer) SaveCluster(c Cluster) error {
	i.dbMtx.Lock()
	defer i.dbMtx.Unlock()

	base := c.Base()

	base.Type = c.Type()
	base.Name = base.ID

	baseFields, err := i.dbMarshalItem("clusters", base, nil)
	if err != nil {
		return err
	}
	baseVStr := make([]string, 0, len(baseFields))
	for idx := range baseFields {
		baseVStr = append(baseVStr, fmt.Sprintf("$%d", idx+1))
	}

	tableName := strings.Join([]string{base.Type, "clusters"}, "_")
	clusterFields, err := i.dbMarshalItem(tableName, c, nil)
	if err != nil {
		return err
	}
	clusterVStr := make([]string, 0, len(clusterFields))
	for idx := range clusterFields {
		clusterVStr = append(clusterVStr, fmt.Sprintf("$%d", idx+1))
	}

	if err != nil {
		return err
	}
	tx, err := i.db.Begin()
	if err != nil {
		return err
	}
	if _, err := tx.Exec(fmt.Sprintf("INSERT INTO clusters VALUES (%s)", strings.Join(baseVStr, ",")), baseFields...); err != nil {
		tx.Rollback()
		return err
	}
	if _, err := tx.Exec(fmt.Sprintf("INSERT INTO %s VALUES (%s)", tableName, strings.Join(clusterVStr, ",")), clusterFields...); err != nil {
		tx.Rollback()
		return err
	}
	return tx.Commit()
}

func (i *Installer) FindBaseCluster(id string) (*BaseCluster, error) {
	i.clustersMtx.RLock()
	for _, c := range i.clusters {
		base := c.Base()
		if base.ID == id {
			i.clustersMtx.RUnlock()
			return base, nil
		}
	}
	i.clustersMtx.RUnlock()

	c := &BaseCluster{ID: id, installer: i}

	err := i.db.QueryRow(`
	SELECT CredentialID, Type, State, NumInstances, ControllerKey, ControllerPin, DashboardLoginToken, CACert, SSHKeyName, DiscoveryToken FROM clusters WHERE ID == $1 AND DeletedAt IS NULL LIMIT 1
  `, c.ID).Scan(&c.CredentialID, &c.Type, &c.State, &c.NumInstances, &c.ControllerKey, &c.ControllerPin, &c.DashboardLoginToken, &c.CACert, &c.SSHKeyName, &c.DiscoveryToken)
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

	if c.Type != "ssh" {
		credential, err := c.FindCredentials()
		if err != nil {
			return nil, err
		}
		c.credential = credential
	}

	return c, nil
}

func (i *Installer) findCachedCluster(id string) (Cluster, error) {
	i.clustersMtx.RLock()
	for _, c := range i.clusters {
		if c.Base().ID == id {
			i.clustersMtx.RUnlock()
			return c, nil
		}
	}
	i.clustersMtx.RUnlock()
	return nil, fmt.Errorf("cluster not found")
}

func (i *Installer) cacheCluster(c Cluster) {
	i.clustersMtx.Lock()
	defer i.clustersMtx.Unlock()
	i.clusters = append(i.clusters, c)
}

func (i *Installer) FindCluster(id string) (cluster Cluster, err error) {
	if c, err := i.findCachedCluster(id); err == nil {
		return c, nil
	}

	defer func() {
		if err == nil {
			i.cacheCluster(cluster)
		}
	}()

	base := &BaseCluster{}
	if err := i.db.QueryRow(`SELECT Type FROM clusters WHERE ID == $1 AND DeletedAt IS NULL`, id).Scan(&base.Type); err != nil {
		return nil, err
	}

	switch base.Type {
	case "aws":
		return i.FindAWSCluster(id)
	case "digital_ocean":
		return i.FindDigitalOceanCluster(id)
	case "azure":
		return i.FindAzureCluster(id)
	case "ssh":
		return i.FindSSHCluster(id)
	default:
		return nil, fmt.Errorf("Invalid cluster type: %s", base.Type)
	}
}

func (i *Installer) FindDigitalOceanCluster(id string) (*DigitalOceanCluster, error) {
	base, err := i.FindBaseCluster(id)
	if err != nil {
		return nil, err
	}

	cluster := &DigitalOceanCluster{
		ClusterID: base.ID,
		base:      base,
	}

	if err := i.db.QueryRow(`SELECT Region, Size, KeyFingerprint FROM digital_ocean_clusters WHERE ClusterID == $1 AND DeletedAt IS NULL LIMIT 1`, base.ID).Scan(&cluster.Region, &cluster.Size, &cluster.KeyFingerprint); err != nil {
		return nil, err
	}

	rows, err := i.db.Query(`SELECT ID FROM digital_ocean_droplets WHERE ClusterID == $1 AND DeletedAt IS NULL`, base.ID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	dropletIDs := make([]int64, 0, base.NumInstances)
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		dropletIDs = append(dropletIDs, id)
	}
	cluster.DropletIDs = dropletIDs

	cluster.SetCreds(base.credential)

	return cluster, nil
}

func (i *Installer) FindAzureCluster(id string) (*AzureCluster, error) {
	base, err := i.FindBaseCluster(id)
	if err != nil {
		return nil, err
	}

	cluster := &AzureCluster{
		ClusterID: base.ID,
		base:      base,
	}

	if err := i.db.QueryRow(`SELECT SubscriptionID, Region, Size FROM azure_clusters WHERE ClusterID == $1 AND DeletedAt IS NULL LIMIT 1`, cluster.ClusterID).Scan(&cluster.SubscriptionID, &cluster.Region, &cluster.Size); err != nil {
		return nil, err
	}

	cluster.SetCreds(base.credential)

	return cluster, nil
}

func (i *Installer) FindSSHCluster(id string) (*SSHCluster, error) {
	base, err := i.FindBaseCluster(id)
	if err != nil {
		return nil, err
	}

	cluster := &SSHCluster{
		ClusterID: base.ID,
		base:      base,
	}

	if err := i.db.QueryRow(`SELECT SSHLogin, TargetsJSON FROM ssh_clusters WHERE ClusterID == $1 AND DeletedAt IS NULL LIMIT 1`, cluster.ClusterID).Scan(&cluster.SSHLogin, &cluster.TargetsJSON); err != nil {
		return nil, err
	}

	if err := json.Unmarshal([]byte(cluster.TargetsJSON), &cluster.Targets); err != nil {
		return nil, err
	}

	return cluster, nil
}

func (i *Installer) FindAWSCluster(id string) (*AWSCluster, error) {
	cluster, err := i.FindBaseCluster(id)
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

	awsCreds, err := awsCluster.FindCredentials()
	if err != nil {
		return nil, err
	}
	awsCluster.creds = awsCreds

	return awsCluster, nil
}

func (i *Installer) DeleteCluster(id string) error {
	cluster, err := i.FindCluster(id)
	if err != nil {
		return err
	}
	go cluster.Delete()
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
	i.removeClusterEvents(id)
}
