package installer

import (
	"bytes"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/flynn/flynn/pkg/azure"
	"github.com/flynn/flynn/pkg/sshkeygen"
	"github.com/flynn/oauth2"
	"golang.org/x/net/context"
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
			azureJSONOAuthClient = azure.OAuth2Config(oc.ClientID, creds.Endpoint, oc.Scope).Client(ctx, token)
		case azure.XMLAPIResource:
			azureXMLOAuthClient = azure.OAuth2Config(oc.ClientID, creds.Endpoint, oc.Scope).Client(ctx, token)
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

func (c *AzureCluster) Run() {
	go func() {
		defer c.base.handleDone()

		steps := []func() error{
			c.createKeyPair,
			c.createResourceGroup,
			c.createTemplateDeployment,
			c.base.allocateDomain,
			c.configureDNS,
			c.base.uploadBackup,
			c.installFlynn,
			c.bootstrap,
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
	if err := c.base.saveField("SSHKeyName", c.base.SSHKeyName); err != nil {
		return err
	}
	return saveSSHKey(keypairName, keypair)
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
	sshConfig, err := c.base.sshConfig()
	if err != nil {
		return err
	}
	targets := make([]*TargetServer, len(c.base.InstanceIPs))
	for i, ip := range c.base.InstanceIPs {
		targets[i] = &TargetServer{
			IP:        ip,
			Port:      "22",
			User:      sshConfig.User,
			SSHConfig: sshConfig,
		}
	}
	bareCluster := &BareCluster{
		Base:    c.base,
		Targets: targets,
	}
	return bareCluster.InstallFlynn()
}

func (c *AzureCluster) bootstrap() error {
	time.Sleep(1 * time.Minute)
	return c.base.bootstrap()
}

func (c *AzureCluster) wrapRequest(runRequest func() error) error {
	authAttemptsRemaining := 3
	for {
		err := runRequest()
		if err == nil || !strings.Contains(err.Error(), "unauthorized_client") || authAttemptsRemaining == 0 {
			return err
		}
		if c.base.HandleAuthenticationFailure(c, err) {
			authAttemptsRemaining--
			continue
		}
		return err
	}
}

func (c *AzureCluster) Delete() {
	prevState := c.base.getState()
	c.base.setState("deleting")

	if prevState != "deleting" {
		c.base.SendLog("Deleting resource group")
		if err := c.wrapRequest(func() error {
			return c.client.DeleteResourceGroup(c.SubscriptionID, c.ClusterID)
		}); err != nil {
			c.base.SendError(err)
			if !c.base.YesNoPrompt(fmt.Sprintf("%s\nWould you like to remove it from the installer?", err.Error())) {
				c.base.setState("error")
				return
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
