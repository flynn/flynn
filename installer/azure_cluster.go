package installer

import (
	"bytes"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/flynn/oauth2"
	"github.com/flynn/flynn/Godeps/_workspace/src/golang.org/x/crypto/ssh"
	"github.com/flynn/flynn/Godeps/_workspace/src/golang.org/x/net/context"
	"github.com/flynn/flynn/pkg/azure"
	"github.com/flynn/flynn/pkg/sshkeygen"
)

func (i *Installer) azureClient(creds *Credential) *azure.Client {
	var azureJSONOAuthClient *http.Client
	var azureXMLOAuthClient *http.Client
	for _, oc := range creds.OAuthCreds {
		ctx := context.WithValue(oauth2.NoContext, oauth2.TokenRefreshNotifier, i.azureTokenRefreshHandler(oc.ClientID, oc.Scope))
		token := &oauth2.Token{
			AccessToken:  oc.AccessToken,
			RefreshToken: oc.RefreshToken,
			Expiry:       *oc.ExpiresAt,
		}
		switch oc.Scope {
		case azure.JSONAPIResource:
			azureJSONOAuthClient = azure.OAuth2Config(oc.ClientID, oc.Scope).Client(ctx, token)
		case azure.XMLAPIResource:
			azureXMLOAuthClient = azure.OAuth2Config(oc.ClientID, oc.Scope).Client(ctx, token)
		}
	}
	return azure.NewClient(azureJSONOAuthClient, azureXMLOAuthClient)
}

func (i *Installer) updateAzureToken(clientID, scope string, token *oauth2.Token) error {
	tx, err := i.db.Begin()
	if err != nil {
		return err
	}
	if _, err := tx.Exec(`UPDATE oauth_credentials SET DeletedAt = now() WHERE ClientID == $1 AND Scope == $2`, clientID, scope); err != nil {
		tx.Rollback()
		return err
	}
	if _, err := tx.Exec(`INSERT INTO oauth_credentials (ClientID, AccessToken, RefreshToken, ExpiresAt, Scope) VALUES ($1, $2, $3, $4, $5);`, clientID, token.AccessToken, token.RefreshToken, token.Expiry, scope); err != nil {
		tx.Rollback()
		return err
	}
	return tx.Commit()
}

func (i *Installer) azureTokenRefreshHandler(clientID, scope string) oauth2.TokenRefreshNotifierFunc {
	return func(token *oauth2.Token) {
		if err := i.updateAzureToken(clientID, scope, token); err != nil {
			fmt.Println(err)
		}
	}
}

func (c *AzureCluster) Type() string {
	return "azure"
}

func (c *AzureCluster) Base() *BaseCluster {
	return c.base
}

func (c *AzureCluster) SetBase(base *BaseCluster) {
	c.base = base
}

func (c *AzureCluster) SetCreds(creds *Credential) error {
	c.base.credential = creds
	c.base.CredentialID = creds.ID
	c.client = c.base.installer.azureClient(creds)
	return nil
}

func (c *AzureCluster) SetDefaultsAndValidate() error {
	c.ClusterID = c.base.ID
	c.base.SSHUsername = "flynn"
	if c.SubscriptionID == "" {
		return errors.New("SubscriptionID must be set")
	}
	if err := c.base.SetDefaultsAndValidate(); err != nil {
		return err
	}
	return nil
}

func (c *AzureCluster) saveField(field string, value interface{}) error {
	c.base.installer.dbMtx.Lock()
	defer c.base.installer.dbMtx.Unlock()
	return c.base.installer.txExec(fmt.Sprintf(`
  UPDATE azure_clusters SET %s = $2 WHERE ClusterID == $1
  `, field), c.ClusterID, value)
}

func (c *AzureCluster) Run() {
	go func() {
		defer c.base.handleDone()

		steps := []func() error{
			c.createKeyPair,
			c.createResourceGroup,
			c.createTemplateDeployment,
			c.base.allocateDomain,
			c.configureDNS,
			c.installFlynn,
			c.bootstrap,
		}

		for _, step := range steps {
			if err := step(); err != nil {
				if c.base.State != "deleting" {
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

func (c *AzureCluster) createKeyPair() error {
	keypairName := "flynn"
	if c.base.SSHKeyName != "" {
		keypairName = c.base.SSHKeyName
	}
	keypair, err := loadSSHKey(keypairName)
	if err == nil {
		c.base.SendLog(fmt.Sprintf("Using saved key pair (%s)", keypairName))
	} else {
		c.base.SendLog("Creating key pair")
		keypair, err = sshkeygen.Generate()
		if err != nil {
			return err
		}
	}
	c.base.SSHKey = keypair
	c.base.SSHKeyName = keypairName
	return nil
}

func (c *AzureCluster) createResourceGroup() error {
	c.base.SendLog("Creating resource group")
	_, err := c.client.CreateResourceGroup(&azure.ResourceGroup{
		SubscriptionID: c.SubscriptionID,
		Name:           c.ClusterID,
		Location:       c.Region,
		Tags: map[string]string{
			"type": "flynn",
		},
	})
	if err != nil {
		return err
	}
	return nil
}

func (c *AzureCluster) createTemplateDeployment() error {
	c.base.SendLog("Creating template deployment")
	type templateData struct {
		Instances []struct{}
	}
	var template bytes.Buffer
	if err := azureTemplate.Execute(&template, &templateData{
		Instances: make([]struct{}, c.base.NumInstances),
	}); err != nil {
		return err
	}
	_, err := c.client.CreateTemplateDeployment(&azure.TemplateDeploymentRequest{
		SubscriptionID:    c.SubscriptionID,
		ResourceGroupName: c.ClusterID,
		Name:              c.ClusterID,
		Parameters: map[string]*azure.TemplateParam{
			"Location":           azure.MustTemplateParam(c.Region),
			"VirtualMachineSize": azure.MustTemplateParam(c.Size),
			"ClusterID":          azure.MustTemplateParam(c.ClusterID),
			"StorageAccountName": azure.MustTemplateParam(fmt.Sprintf("%sstorage", strings.Replace(c.ClusterID, "-", "", -1))),
			"NumInstances":       azure.MustTemplateParam(c.base.NumInstances),
			"VirtualMachineUser": azure.MustTemplateParam(c.base.SSHUsername),
			"SSHPublicKey":       azure.MustTemplateParam(string(c.base.SSHKey.PublicKey)),
		},
		Template: template.Bytes(),
	})
	if err != nil {
		return err
	}
	c.base.SendLog("Waiting for template deployment to complete")
	type PublicIPAddressesOutput struct {
		Value []string `json:"value"`
	}
	type Output struct {
		PublicIPAddresses PublicIPAddressesOutput `json:"publicIPAddresses"`
	}
	var outputs Output
	if err := c.client.WaitForTemplateDeployment(c.SubscriptionID, c.ClusterID, c.ClusterID, &outputs); err != nil {
		return err
	}
	c.base.SendLog("Fetching outputs")
	type PublicIPAddress struct {
		ProvisioningState        string `json:"provisioningState"`
		IPAddress                string `json:"ipAddress"`
		PublicIPAllocationMethod string `json:"publicIPAllocationMethod"`
		IdleTimeoutInMinutes     int    `json:"idleTimeoutInMinutes"`
	}
	instanceIPs := make([]string, 0, c.base.NumInstances)
	for _, resourceID := range outputs.PublicIPAddresses.Value {
		var publicIP PublicIPAddress
		if err := c.client.Get(resourceID, &publicIP); err != nil {
			return err
		}
		instanceIPs = append(instanceIPs, publicIP.IPAddress)
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

func (c *AzureCluster) configureDNS() error {
	c.base.SendLog("Configuring DNS")
	return c.base.Domain.ConfigureIPAddresses(c.base.InstanceIPs)
}

func (c *AzureCluster) installFlynn() error {
	c.base.SendLog("Installing flynn")

	startScript, discoveryToken, err := c.base.genStartScript(c.base.NumInstances)
	if err != nil {
		return err
	}
	c.base.DiscoveryToken = discoveryToken
	if err := c.base.saveField("DiscoveryToken", discoveryToken); err != nil {
		return err
	}
	c.startScript = startScript

	sshConfig, err := c.base.sshConfig()
	if err != nil {
		return err
	}

	instanceIPs := c.base.InstanceIPs
	errChan := make(chan error, len(instanceIPs))
	ops := []func(*ssh.ClientConfig, string) error{
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

func (c *AzureCluster) instanceInstallFlynn(sshConfig *ssh.ClientConfig, ipAddress string) error {
	attemptsRemaining := 3
	for {
		c.base.SendLog(fmt.Sprintf("Installing flynn on %s", ipAddress))
		cmd := "curl -fsSL -o /tmp/install-flynn https://dl.flynn.io/install-flynn && sudo bash /tmp/install-flynn"
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

func (c *AzureCluster) instanceStartFlynn(sshConfig *ssh.ClientConfig, ipAddress string) error {
	c.base.SendLog(fmt.Sprintf("Starting flynn on %s", ipAddress))
	cmd := fmt.Sprintf(`echo "%s" | base64 -d | sudo bash`, c.startScript)
	return c.base.instanceRunCmd(cmd, sshConfig, ipAddress)
}

func (c *AzureCluster) bootstrap() error {
	time.Sleep(1 * time.Minute)
	return c.base.bootstrap()
}

func (c *AzureCluster) Delete() {
	prevState := c.base.State
	c.base.setState("deleting")

	if prevState != "deleting" {
		c.base.SendLog("Deleting resource group")
		if err := c.client.DeleteResourceGroup(c.SubscriptionID, c.ClusterID); err != nil {
			c.base.SendError(err)
			return
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
