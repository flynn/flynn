package installer

import (
	"crypto/md5"
	"crypto/rsa"
	"errors"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/digitalocean/godo"
	"github.com/flynn/flynn/pkg/sshkeygen"
	"golang.org/x/crypto/ssh"
	"golang.org/x/oauth2"
)

type doTokenSource struct {
	AccessToken string
}

func (t *doTokenSource) Token() (*oauth2.Token, error) {
	return &oauth2.Token{
		AccessToken: t.AccessToken,
	}, nil
}

func digitalOceanClient(creds *Credential) *godo.Client {
	return godo.NewClient(oauth2.NewClient(oauth2.NoContext, &doTokenSource{creds.Secret}))
}

func (c *DigitalOceanCluster) Type() string {
	return "digital_ocean"
}

func (c *DigitalOceanCluster) Base() *BaseCluster {
	return c.base
}

func (c *DigitalOceanCluster) SetBase(base *BaseCluster) {
	c.base = base
}

func (c *DigitalOceanCluster) SetCreds(creds *Credential) error {
	c.base.credential = creds
	c.base.CredentialID = creds.ID
	c.client = digitalOceanClient(creds)
	return nil
}

func (c *DigitalOceanCluster) SetDefaultsAndValidate() error {
	c.ClusterID = c.base.ID
	c.base.SSHUsername = "root"
	if err := c.base.SetDefaultsAndValidate(); err != nil {
		return err
	}
	return nil
}

func (c *DigitalOceanCluster) saveField(field string, value interface{}) error {
	c.base.installer.dbMtx.Lock()
	defer c.base.installer.dbMtx.Unlock()
	return c.base.installer.txExec(fmt.Sprintf(`
  UPDATE digital_ocean_clusters SET %s = $2 WHERE ClusterID == $1
  `, field), c.ClusterID, value)
}

func (c *DigitalOceanCluster) saveDropletIDs() error {
	c.base.installer.dbMtx.Lock()
	defer c.base.installer.dbMtx.Unlock()
	tx, err := c.base.installer.db.Begin()
	if err != nil {
		return err
	}
	for _, id := range c.DropletIDs {
		_, err = tx.Exec(`INSERT INTO digital_ocean_droplets (ClusterID, ID) VALUES ($1, $2)`, c.base.ID, id)
		if err != nil {
			tx.Rollback()
			return err
		}
	}
	return tx.Commit()
}

func (c *DigitalOceanCluster) Run() {
	go func() {
		defer c.base.handleDone()

		steps := []func() error{
			c.createKeyPair,
			c.base.allocateDomain,
			c.configureDNS,
			c.createDroplets,
			c.fetchInstanceIPs,
			c.configureDomain,
			c.base.uploadBackup,
			c.installFlynn,
			c.bootstrap,
			c.base.waitForDNS,
		}

		for _, step := range steps {
			if err := step(); err != nil {
				if c.base.getState() != "deleting" {
					c.base.setState("error")
					c.base.SendError(err)
				}
				return
			}
		}

		c.base.setState("running")

		if err := c.base.configureCLI(); err != nil {
			c.base.SendLog(fmt.Sprintf("WARNING: Failed to configure CLI: %s", err))
		}
	}()
}

func (c *DigitalOceanCluster) createKeyPair() error {
	keypairName := "flynn"
	if c.base.SSHKeyName != "" {
		keypairName = c.base.SSHKeyName
	}
	if err := c.loadKeyPair(keypairName); err == nil {
		c.base.SendLog(fmt.Sprintf("Using saved key pair (%s)", c.base.SSHKeyName))
		return nil
	}

	keypair, err := loadSSHKey(keypairName)
	if err == nil {
		c.base.SendLog("Importing key pair")
	} else {
		c.base.SendLog("Creating key pair")
		keypair, err = sshkeygen.Generate()
		if err != nil {
			return err
		}
	}

	key, _, err := c.client.Keys.Create(&godo.KeyCreateRequest{
		Name:      keypairName,
		PublicKey: string(keypair.PublicKey),
	})
	if err != nil {
		return err
	}

	c.base.SSHKey = keypair
	c.base.SSHKeyName = keypairName
	c.KeyFingerprint = key.Fingerprint
	if err := c.saveField("KeyFingerprint", c.KeyFingerprint); err != nil {
		return err
	}

	if err := c.base.saveField("SSHKeyName", c.base.SSHKeyName); err != nil {
		return err
	}

	err = saveSSHKey(keypairName, keypair)
	if err != nil {
		return err
	}
	return nil
}

func (c *DigitalOceanCluster) loadKeyPair(name string) error {
	keypair, err := loadSSHKey(name)
	if err != nil {
		return err
	}
	fingerprint, err := c.fingerprintSSHKey(keypair.PrivateKey)
	key, _, err := c.client.Keys.GetByFingerprint(fingerprint)
	if err != nil {
		return err
	}
	c.base.SSHKey = keypair
	c.base.SSHKeyName = key.Name
	c.KeyFingerprint = fingerprint
	if err := c.saveField("KeyFingerprint", c.KeyFingerprint); err != nil {
		return err
	}
	return saveSSHKey(c.base.SSHKeyName, keypair)
}

func (c *DigitalOceanCluster) fingerprintSSHKey(privateKey *rsa.PrivateKey) (string, error) {
	rsaPubKey, err := ssh.NewPublicKey(&privateKey.PublicKey)
	if err != nil {
		return "", err
	}
	md5Data := md5.Sum(rsaPubKey.Marshal())
	strbytes := make([]string, len(md5Data))
	for i, b := range md5Data {
		strbytes[i] = fmt.Sprintf("%02x", b)
	}
	return strings.Join(strbytes, ":"), nil
}

func (c *DigitalOceanCluster) configureDNS() error {
	c.base.SendLog("Configuring DNS")
	nameServers := []string{
		"ns1.digitalocean.com",
		"ns2.digitalocean.com",
		"ns3.digitalocean.com",
	}
	if err := c.base.Domain.Configure(nameServers); err != nil {
		return err
	}
	return nil
}

func (c *DigitalOceanCluster) createDroplets() error {
	numInstances := int(c.base.NumInstances)
	actionIDs := make([]int, numInstances)
	for i := 0; i < numInstances; i++ {
		name := c.base.Name
		if numInstances > 1 {
			name = fmt.Sprintf("%s-%d", name, i)
		}
		actionID, err := c.createDroplet(name)
		if err != nil {
			return err
		}
		actionIDs[i] = *actionID
	}
	if err := c.saveDropletIDs(); err != nil {
		return err
	}
	for _, actionID := range actionIDs {
		if err := c.waitForActionComplete(actionID); err != nil {
			return err
		}
	}
	return nil
}

func (c *DigitalOceanCluster) createDroplet(name string) (*int, error) {
	c.base.SendLog(fmt.Sprintf("Creating droplet %s", name))
	droplet, res, err := c.client.Droplets.Create(&godo.DropletCreateRequest{
		Name:   name,
		Region: c.Region,
		Size:   c.Size,
		Image: godo.DropletCreateImage{
			Slug: "ubuntu-16-04-x64",
		},
		SSHKeys: []godo.DropletCreateSSHKey{{
			Fingerprint: c.KeyFingerprint,
		}},
	})
	if err != nil {
		return nil, err
	}
	c.DropletIDs = append(c.DropletIDs, int64(droplet.ID))
	for _, a := range res.Links.Actions {
		if a.Rel == "create" {
			return &a.ID, nil
		}
	}
	return nil, errors.New("Unable to locate create action ID")
}

func (c *DigitalOceanCluster) waitForActionComplete(actionID int) error {
	fetchAction := func() (*godo.Action, error) {
		action, _, err := c.client.Actions.Get(actionID)
		if err != nil {
			return nil, err
		}
		return action, nil
	}
	for {
		action, err := fetchAction()
		if err != nil {
			return err
		}
		switch action.Status {
		case "completed":
			return nil
		case "errored":
			return errors.New("Droplet create failed")
		}
		time.Sleep(1 * time.Second)
	}
}

func (c *DigitalOceanCluster) fetchInstanceIPs() error {
	droplets, err := c.fetchDroplets()
	if err != nil {
		return err
	}
	instanceIPs := make([]string, 0, len(droplets))
	for _, d := range droplets {
		for _, n := range d.Networks.V4 {
			instanceIPs = append(instanceIPs, n.IPAddress)
		}
	}
	if int64(len(instanceIPs)) != c.base.NumInstances {
		return fmt.Errorf("Expected %d instances, but found %d", c.base.NumInstances, len(instanceIPs))
	}
	c.base.InstanceIPs = instanceIPs
	if err := c.base.saveInstanceIPs(); err != nil {
		return err
	}
	return nil
}

func (c *DigitalOceanCluster) fetchDroplets() ([]*godo.Droplet, error) {
	c.base.SendLog(fmt.Sprintf("Fetching droplets for %s", c.base.Name))
	droplets := make([]*godo.Droplet, 0, c.base.NumInstances)
	for _, id := range c.DropletIDs {
		dr, _, err := c.client.Droplets.Get(int(id))
		if err != nil {
			return nil, err
		}
		droplets = append(droplets, dr)
	}
	return droplets, nil
}

func (c *DigitalOceanCluster) configureDomain() error {
	c.base.SendLog("Configuring domain")
	instanceIP := c.base.InstanceIPs[0]
	dr, _, err := c.client.Domains.Create(&godo.DomainCreateRequest{
		Name:      c.base.Domain.Name,
		IPAddress: instanceIP,
	})
	if err != nil {
		return err
	}
	domainName := dr.Name
	for i, ip := range c.base.InstanceIPs {
		if i == 0 {
			// An A record already exists via the create domain request
			continue
		}
		_, _, err := c.client.Domains.CreateRecord(domainName, &godo.DomainRecordEditRequest{
			Type: "A",
			Name: fmt.Sprintf("%s.", domainName),
			Data: ip,
		})
		if err != nil {
			return err
		}
	}
	_, _, err = c.client.Domains.CreateRecord(domainName, &godo.DomainRecordEditRequest{
		Type: "CNAME",
		Name: fmt.Sprintf("*.%s.", domainName),
		Data: fmt.Sprintf("%s.", domainName),
	})
	return err
}

func (c *DigitalOceanCluster) installFlynn() error {
	c.base.SendLog("Installing flynn")

	startScript, discoveryToken, err := c.base.genStartScript(c.base.NumInstances, "")
	if err != nil {
		return err
	}
	c.base.DiscoveryToken = discoveryToken
	if err := c.base.saveField("DiscoveryToken", discoveryToken); err != nil {
		return err
	}
	c.startScript = startScript

	iptablesConfigScript, err := c.base.genIPTablesConfigScript()
	if err != nil {
		return err
	}
	c.iptablesConfigScript = iptablesConfigScript

	sshConfig, err := c.base.sshConfig()
	if err != nil {
		return err
	}

	instanceIPs := c.base.InstanceIPs
	errChan := make(chan error, len(instanceIPs))
	ops := []func(*ssh.ClientConfig, string) error{
		c.instanceWaitForSSH,
		c.instanceConfigureIPTables,
		c.instanceInstallFlynn,
		c.instanceStartFlynn,
	}
	for _, op := range ops {
		for _, ipAddress := range instanceIPs {
			go func(ipAddress string) {
				errChan <- op(sshConfig, ipAddress)
			}(ipAddress)
		}
		for range instanceIPs {
			if err := <-errChan; err != nil {
				return err
			}
		}
	}
	return nil
}

func (c *DigitalOceanCluster) instanceWaitForSSH(sshConfig *ssh.ClientConfig, ipAddress string) error {
	c.base.SendLog(fmt.Sprintf("Waiting for ssh on %s", ipAddress))
	timeout := time.After(5 * time.Minute)
	for {
		sshConn, err := ssh.Dial("tcp", ipAddress+":22", sshConfig)
		if err != nil {
			if _, ok := err.(*net.OpError); ok {
				select {
				case <-time.After(5 * time.Second):
					continue
				case <-timeout:
					return err
				}
			}
			return err
		}
		sshConn.Close()
		return nil
	}
}

func (c *DigitalOceanCluster) instanceConfigureIPTables(sshConfig *ssh.ClientConfig, ipAddress string) error {
	c.base.SendLog(fmt.Sprintf("Configuring iptables firewall on %s", ipAddress))
	return c.base.instanceRunCmd(c.iptablesConfigScript, sshConfig, ipAddress)
}

func (c *DigitalOceanCluster) instanceInstallFlynn(sshConfig *ssh.ClientConfig, ipAddress string) error {
	attemptsRemaining := 3
	for {
		c.base.SendLog(fmt.Sprintf("Installing flynn on %s", ipAddress))
		cmd := "curl -fsSL -o /tmp/install-flynn https://dl.flynn.io/install-flynn && sudo bash /tmp/install-flynn --clean"
		if c.base.ReleaseChannel != "" {
			cmd = fmt.Sprintf("%s --channel %s", cmd, c.base.ReleaseChannel)
		}
		if c.base.ReleaseVersion != "" {
			cmd = fmt.Sprintf("%s --version %s", cmd, c.base.ReleaseVersion)
		}
		err := c.base.instanceRunCmd(cmd, sshConfig, ipAddress)
		if err != nil {
			if attemptsRemaining > 0 {
				attemptsRemaining--
				time.Sleep(10 * time.Second)
				continue
			}
			return err
		}
		return nil
	}
}

func (c *DigitalOceanCluster) instanceStartFlynn(sshConfig *ssh.ClientConfig, ipAddress string) error {
	c.base.SendLog(fmt.Sprintf("Starting flynn on %s", ipAddress))
	cmd := fmt.Sprintf(`echo "%s" | base64 -d | bash`, c.startScript)
	return c.base.instanceRunCmd(cmd, sshConfig, ipAddress)
}

func (c *DigitalOceanCluster) bootstrap() error {
	return c.base.bootstrap()
}

func (c *DigitalOceanCluster) wrapRequest(runRequest func() (*godo.Response, error)) (*godo.Response, error) {
	authAttemptsRemaining := 3
	for {
		res, err := runRequest()
		errRes, ok := err.(*godo.ErrorResponse)
		if !ok || authAttemptsRemaining == 0 {
			return res, err
		}
		if errRes.Response.StatusCode == 401 {
			if c.base.HandleAuthenticationFailure(c, err) {
				authAttemptsRemaining--
				continue
			}
		}
		return res, err
	}
}

func (c *DigitalOceanCluster) Delete() {
	prevState := c.base.getState()
	c.base.setState("deleting")

	if c.base.Domain != nil {
		if _, err := c.wrapRequest(func() (*godo.Response, error) {
			return c.client.Domains.Delete(c.base.Domain.Name)
		}); err != nil {
			c.base.SendError(err)
		}
	} else {
		c.base.SendLog("Skipping domain deletion")
	}

	if len(c.DropletIDs) > 0 {
		for _, id := range c.DropletIDs {
			if res, err := c.wrapRequest(func() (*godo.Response, error) {
				return c.client.Droplets.Delete(int(id))
			}); err != nil {
				if res.StatusCode == 404 {
					continue
				}
				c.base.SendError(err)
				if !c.base.YesNoPrompt(fmt.Sprintf("Error deleting droplet: %s\nWould you like to remove it from the installer?", err)) {
					c.base.setState(prevState)
					return
				}
			}
		}
	}

	if err := c.base.MarkDeleted(); err != nil {
		c.base.SendError(err)
	}
	c.base.sendEvent(&Event{
		ClusterID:   c.base.ID,
		Type:        "cluster_state",
		Description: "deleted",
	})
}
