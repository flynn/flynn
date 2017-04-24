package installer

import (
	"bufio"
	"bytes"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"text/template"
	"time"

	"github.com/flynn/flynn/bootstrap/discovery"
	cfg "github.com/flynn/flynn/cli/config"
	cc "github.com/flynn/flynn/controller/client"
	"github.com/flynn/flynn/controller/client/v1"
	ct "github.com/flynn/flynn/controller/types"
	"github.com/flynn/flynn/router/types"
	"golang.org/x/crypto/ssh"
)

func (c *BaseCluster) FindCredentials() (*Credential, error) {
	return c.installer.FindCredentials(c.CredentialID)
}

func (c *BaseCluster) HandleAuthenticationFailure(cluster Cluster, err error) bool {
	credentialID := c.CredentialPrompt("Authentication failed, please select a new credential to continue")
	credential, err := c.installer.FindCredentials(credentialID)
	if err != nil {
		c.SendError(fmt.Errorf("error finding credential with ID %s: %s", credentialID, err))
		return false
	}
	if err := cluster.SetCreds(credential); err != nil {
		c.SendError(fmt.Errorf("error setting credential: %s", err))
		return false
	}
	if err := c.installer.SaveCluster(cluster); err != nil {
		c.SendError(fmt.Errorf("error saving cluster: %s", err))
		return false
	}
	c.installer.SendEvent(&Event{
		Type:      "cluster_update",
		ClusterID: c.ID,
	})
	return true
}

func (c *BaseCluster) ReceiveBackup(backup io.Reader, size int) error {
	cbr := c.NewBackupReceiver(backup, size)
	defer cbr.Close()
	c.backupMtx.Lock()
	c.backup = cbr
	c.backupMtx.Unlock()
	return cbr.Wait()
}

func (c *BaseCluster) IsRestoringBackup() bool {
	return c.HasBackup
}

func (c *BaseCluster) saveField(field string, value interface{}) error {
	c.installer.dbMtx.Lock()
	defer c.installer.dbMtx.Unlock()
	return c.installer.txExec(fmt.Sprintf("UPDATE clusters SET %s = $2 WHERE ID == $1", field), c.ID, value)
}

func (c *BaseCluster) saveDomain() error {
	c.installer.dbMtx.Lock()
	defer c.installer.dbMtx.Unlock()
	return c.installer.txExec(`
  INSERT INTO domains (ClusterID, Name, Token) VALUES ($1, $2, $3);
  `, c.ID, c.Domain.Name, c.Domain.Token)
}

func (c *BaseCluster) saveInstanceIPs() error {
	c.installer.dbMtx.Lock()
	defer c.installer.dbMtx.Unlock()

	tx, err := c.installer.db.Begin()
	if err != nil {
		return err
	}
	insertStmt := "INSERT INTO instances (ClusterID, IP) VALUES ($1, $2)"
	for _, ip := range c.InstanceIPs {
		if _, err := tx.Exec(insertStmt, c.ID, ip); err != nil {
			tx.Rollback()
			return err
		}
	}
	return tx.Commit()
}

func (c *BaseCluster) setState(state string) {
	c.stateMtx.Lock()
	c.State = state
	c.stateMtx.Unlock()
	go func() {
		if err := c.saveField("State", state); err != nil {
			c.installer.logger.Debug(fmt.Sprintf("Error saving cluster State: %s", err.Error()))
		}
		if state == "running" {
			if err := c.installer.txExec(`
				UPDATE events SET DeletedAt = now() WHERE (Type == "log" OR Type == "progress") AND ClusterID == $1;
			`, c.ID); err != nil {
				c.installer.logger.Debug(fmt.Sprintf("Error updating events: %s", err.Error()))
			}
			c.installer.removeClusterLogEvents(c.ID)
		}
	}()
	go c.sendEvent(&Event{
		ClusterID:   c.ID,
		Type:        "cluster_state",
		Description: state,
	})
}

func (c *BaseCluster) getState() string {
	c.stateMtx.RLock()
	defer c.stateMtx.RUnlock()
	return c.State
}

func (c *BaseCluster) MarkDeleted() (err error) {
	c.installer.dbMtx.Lock()
	defer c.installer.dbMtx.Unlock()

	var tx *sql.Tx
	tx, err = c.installer.db.Begin()
	if err != nil {
		return
	}

	if _, err = tx.Exec(`UPDATE prompts SET DeletedAt = now() WHERE ID IN (SELECT ResourceID FROM events WHERE ClusterID == $1 AND ResourceType == "prompt")`, c.ID); err != nil {
		tx.Rollback()
		return
	}

	if _, err = tx.Exec(`UPDATE events SET DeletedAt = now() WHERE ClusterID == $1`, c.ID); err != nil {
		tx.Rollback()
		return
	}

	if _, err = tx.Exec(`UPDATE domains SET DeletedAt = now() WHERE ClusterID == $1`, c.ID); err != nil {
		tx.Rollback()
		return
	}

	if _, err = tx.Exec(`UPDATE instances SET DeletedAt = now() WHERE ClusterID == $1`, c.ID); err != nil {
		tx.Rollback()
		return
	}

	if _, err = tx.Exec(`UPDATE clusters SET DeletedAt = now() WHERE ID == $1`, c.ID); err != nil {
		tx.Rollback()
		return
	}

	if _, err = tx.Exec(`UPDATE aws_clusters SET DeletedAt = now() WHERE ClusterID == $1`, c.ID); err != nil {
		tx.Rollback()
		return
	}

	if _, err = tx.Exec(`UPDATE digital_ocean_clusters SET DeletedAt = now() WHERE ClusterID == $1`, c.ID); err != nil {
		tx.Rollback()
		return
	}

	if _, err = tx.Exec(`UPDATE digital_ocean_droplets SET DeletedAt = now() WHERE ClusterID == $1`, c.ID); err != nil {
		tx.Rollback()
		return
	}

	if _, err = tx.Exec(`UPDATE ssh_clusters SET DeletedAt = now() WHERE ClusterID == $1`, c.ID); err != nil {
		tx.Rollback()
		return
	}

	c.installer.ClusterDeleted(c.ID)
	err = tx.Commit()
	return
}

func (c *BaseCluster) SetDefaultsAndValidate() error {
	if c.NumInstances == 0 {
		c.NumInstances = 1
	}
	c.InstanceIPs = make([]string, 0, c.NumInstances)
	c.passwordCache = make(map[string]string)
	return c.validateInputs()
}

func (c *BaseCluster) validateInputs() error {
	if c.NumInstances <= 0 {
		return fmt.Errorf("You must specify at least one instance")
	}

	if c.NumInstances > 5 {
		return fmt.Errorf("Maximum of 5 instances exceeded")
	}

	if c.NumInstances == 2 {
		return fmt.Errorf("You must specify 1 or 3+ instances, not 2")
	}

	return nil
}

func (c *BaseCluster) StackAddCmd() (string, error) {
	if c.ControllerKey == "" || c.ControllerPin == "" || c.Domain == nil || c.Domain.Name == "" {
		return "", fmt.Errorf("Not enough data present")
	}
	return fmt.Sprintf("flynn cluster add -p %s default %s %s", c.ControllerPin, c.Domain.Name, c.ControllerKey), nil
}

func (c *BaseCluster) ClusterConfig() *cfg.Cluster {
	return &cfg.Cluster{
		Name:          c.Name,
		ControllerURL: "https://controller." + c.Domain.Name,
		GitURL:        "https://git." + c.Domain.Name,
		Key:           c.ControllerKey,
		TLSPin:        c.ControllerPin,
	}
}

func (c *BaseCluster) DashboardLoginMsg() (string, error) {
	if c.DashboardLoginToken == "" || c.Domain == nil || c.Domain.Name == "" {
		return "", fmt.Errorf("Not enough data present")
	}
	return fmt.Sprintf("The built-in dashboard can be accessed at http://dashboard.%s with login token %s", c.Domain.Name, c.DashboardLoginToken), nil
}

func (c *BaseCluster) allocateDomain() error {
	c.SendLog("Allocating domain")
	domain, err := AllocateDomain()
	if err != nil {
		return err
	}
	domain.ClusterID = c.ID
	c.Domain = domain
	return c.saveDomain()
}

func (c *BaseCluster) instanceRunCmd(cmd string, sshConfig *ssh.ClientConfig, ipAddress string) error {
	sshConn, err := ssh.Dial("tcp", ipAddress+":22", sshConfig)
	if err != nil {
		return err
	}
	defer sshConn.Close()
	return c.instanceRunCmdWithClient(cmd, sshConn, sshConfig.User, ipAddress)
}

func (c *BaseCluster) Abort() {
	c.aborted = true
}

func (c *BaseCluster) IsAborted() bool {
	return c.aborted
}

func (c *BaseCluster) instanceRunCmdWithClient(cmd string, sshConn *ssh.Client, user, ipAddress string) error {
	c.SendLog(fmt.Sprintf("Running `%s` on %s", cmd, ipAddress))
	sudoPrompt := "<SUDO_PROMPT>"
	cmd = strings.Replace(cmd, "sudo ", fmt.Sprintf("sudo -S --prompt='%s\n' ", sudoPrompt), -1)

	sess, err := sshConn.NewSession()
	if err != nil {
		return err
	}
	stdin, err := sess.StdinPipe()
	if err != nil {
		return err
	}
	stdout, err := sess.StdoutPipe()
	if err != nil {
		return err
	}
	stderr, err := sess.StderrPipe()
	if err != nil {
		return err
	}
	if err = sess.Start(cmd); err != nil {
		return err
	}

	doneChan := make(chan struct{})
	defer close(doneChan)

	go func() {
		scanner := bufio.NewScanner(stdout)
		var prevLine string
		for scanner.Scan() {
			select {
			case _, _ = <-doneChan:
				return
			default:
			}

			line := scanner.Text()
			c.SendLog(line)

			// Handle prompt to remove Flynn when installing using --clean
			flynnRemovePromptMsgs := []string{
				"About to stop Flynn and remove all existing data",
				"Are you sure this is what you want?",
			}
			if strings.Contains(prevLine, flynnRemovePromptMsgs[0]) && strings.Contains(line, flynnRemovePromptMsgs[1]) {
				answer := "no"
				if c.YesNoPrompt("If Flynn is already installed, it will be stopped and removed along with all data before installing the latest version. Would you like to proceed?") {
					answer = "yes"
				} else {
					c.Abort()
				}
				if _, err := fmt.Fprintf(stdin, "%s\n", answer); err != nil {
					c.SendLog(err.Error())
				}
			}
			prevLine = line
		}
	}()
	go func() {
		passwordPrompt := func(msg string, useCache bool) {
			c.passwordPromptMtx.Lock()
			var password string
			var ok bool
			if useCache {
				password, ok = c.passwordCache[ipAddress]
			}
			if !ok || password == "" {
				password = c.PromptProtectedInput(msg)
				c.passwordCache[ipAddress] = password
			} else {
				c.SendLog("Using cached password")
			}
			if _, err := fmt.Fprintf(stdin, "%s\n", password); err != nil {
				c.SendLog(err.Error())
			}
			c.passwordPromptMtx.Unlock()
		}

		scanner := bufio.NewScanner(stderr)
		var prevLine string
		for scanner.Scan() {
			select {
			case _, _ = <-doneChan:
				return
			default:
			}

			line := scanner.Text()
			c.SendLog(line)

			msg := fmt.Sprintf("Please enter your sudo password for %s@%s", user, ipAddress)
			if prevLine == sudoPrompt && line == "Sorry, try again." {
				passwordPrompt(fmt.Sprintf("%s\n%s", line, msg), false)
			} else if line == sudoPrompt {
				passwordPrompt(msg, true)
			}
			prevLine = line
		}
	}()

	return sess.Wait()
}

func (c *BaseCluster) uploadDebugInfo(t *TargetServer) {
	cmd := "sudo flynn-host collect-debug-info"
	if t.SSHClient == nil {
		var err error
		t.SSHClient, err = ssh.Dial("tcp", net.JoinHostPort(t.IP, t.Port), t.SSHConfig)
		if err != nil {
			c.SendLog(fmt.Sprintf("Error connecting to %s:%s: %s", t.IP, t.Port, err))
			return
		}
	}
	if err := c.instanceRunCmdWithClient(cmd, t.SSHClient, t.User, t.IP); err != nil {
		c.SendLog(fmt.Sprintf("Error running %s: %s", cmd, err))
	}
}

func (c *BaseCluster) sshConfig() (*ssh.ClientConfig, error) {
	if c.SSHKey == nil {
		return nil, errors.New("No SSHKey found")
	}
	signer, err := ssh.NewSignerFromKey(c.SSHKey.PrivateKey)
	if err != nil {
		return nil, err
	}
	sshConfig := &ssh.ClientConfig{
		User: c.SSHUsername,
		Auth: []ssh.AuthMethod{ssh.PublicKeys(signer)},
	}
	return sshConfig, nil
}

func (c *BaseCluster) attemptSSHConnectionForTarget(t *TargetServer) error {
	attempts := 0
	maxAttempts := 30
	for {
		var err error
		t.SSHClient, err = ssh.Dial("tcp", net.JoinHostPort(t.IP, t.Port), t.SSHConfig)
		if err != nil {
			if attempts < maxAttempts {
				attempts++
				c.SendLog(err.Error())
				time.Sleep(time.Second)
				continue
			}
			return err
		}
		break
	}
	return nil
}

func (c *BaseCluster) uploadBackup() error {
	c.backupMtx.RLock()
	if c.backup == nil {
		c.backupMtx.RUnlock()
		return nil
	}
	c.backupMtx.RUnlock()

	// upload to bootstrap instance
	ipAddress := c.InstanceIPs[0]

	sshConfig, err := c.sshConfig()
	if err != nil {
		return err
	}

	target := &TargetServer{
		User:      c.SSHUsername,
		IP:        ipAddress,
		Port:      "22",
		SSHConfig: sshConfig,
	}
	defer func() {
		if target.SSHClient != nil {
			target.SSHClient.Close()
		}
	}()

	return c.uploadBackupToTargetWithRetry(target)
}

func (c *BaseCluster) uploadBackupToTargetWithRetry(t *TargetServer) error {
	c.backupMtx.RLock()
	if c.backup == nil {
		c.backupMtx.RUnlock()
		return nil
	}
	c.backupMtx.RUnlock()

	err := c.uploadBackupToTarget(t)
	if err != nil {
		if _, ok := err.(readBackupError); ok && c.YesNoPrompt(fmt.Sprintf("Error uploading backup:\n%s\n\nWould you like to try again with a different file?", err)) {
			size, file, readFileErrChan := c.PromptFileInput("Please select a backup file to restore")
			c.backupMtx.Lock()
			c.backup.Close()
			c.backup = c.NewBackupReceiver(file, size)
			c.backupMtx.Unlock()
			go func() {
				c.backupMtx.RLock()
				b := c.backup
				c.backupMtx.RUnlock()
				readFileErrChan <- b.Wait()
			}()
			return c.uploadBackupToTargetWithRetry(t)
		}
	}
	c.backup.UploadComplete(err)
	return err
}

func (c *BaseCluster) uploadBackupToTarget(t *TargetServer) error {
	c.SendLog("Uploading backup")

	c.SendProgress(&ProgressEvent{
		ID:          "upload-backup",
		Description: "Upload starting...",
		Percent:     0,
	})

	if t.SSHClient == nil {
		if err := c.attemptSSHConnectionForTarget(t); err != nil {
			return err
		}
	}

	sess, err := t.SSHClient.NewSession()
	if err != nil {
		return err
	}
	stdin, err := sess.StdinPipe()
	if err != nil {
		return err
	}
	sess.Stdout = os.Stdout
	sess.Stderr = os.Stderr

	c.backupPath = "/tmp/flynn-backup.tar"
	if err := sess.Start(fmt.Sprintf("cat > %s", c.backupPath)); err != nil {
		return err
	}

	var uploadErr error
	go func() {
		c.backupMtx.RLock()
		b := c.backup
		c.backupMtx.RUnlock()
		if _, err := io.Copy(stdin, b); err != nil {
			uploadErr = err
		}
		stdin.Close()
	}()

	var backupErr error
	go func() {
		c.backupMtx.RLock()
		b := c.backup
		c.backupMtx.RUnlock()
		if err := b.Wait(); err != nil {
			backupErr = err
			stdin.Close()
		}
	}()

	if err := sess.Wait(); err != nil {
		if backupErr != nil {
			return backupErr
		}
		return err
	}
	if backupErr != nil {
		return backupErr
	}
	if uploadErr != nil {
		return uploadErr
	}

	c.SendProgress(&ProgressEvent{
		ID:          "upload-backup",
		Description: "Upload complete",
		Percent:     100,
	})

	return nil
}

type stepInfo struct {
	ID        string           `json:"id"`
	Action    string           `json:"action"`
	Data      *json.RawMessage `json:"data"`
	State     string           `json:"state"`
	Error     string           `json:"error,omitempty"`
	Timestamp time.Time        `json:"ts"`
}

func (c *BaseCluster) bootstrap() error {
	// bootstrap only needs to run on one instance
	ipAddress := c.InstanceIPs[0]

	sshConfig, err := c.sshConfig()
	if err != nil {
		return err
	}

	target := &TargetServer{
		User:      c.SSHUsername,
		IP:        ipAddress,
		Port:      "22",
		SSHConfig: sshConfig,
	}
	defer func() {
		if target.SSHClient != nil {
			target.SSHClient.Close()
		}
	}()

	return c.bootstrapTarget(target)
}

func (c *BaseCluster) bootstrapTarget(t *TargetServer) error {
	c.SendLog("Running bootstrap")

	if t.SSHClient == nil {
		if err := c.attemptSSHConnectionForTarget(t); err != nil {
			return err
		}
	}

	sess, err := t.SSHClient.NewSession()
	if err != nil {
		return err
	}
	stdout, err := sess.StdoutPipe()
	if err != nil {
		return err
	}
	sess.Stderr = os.Stderr
	clusterDomain := c.Domain.Name
	if c.backupPath != "" {
		clusterDomain = c.oldDomain
	}
	cmd := fmt.Sprintf("CLUSTER_DOMAIN=%s flynn-host bootstrap --timeout 600 --min-hosts=%d --discovery=%s --json", clusterDomain, c.NumInstances, c.DiscoveryToken)
	if c.backupPath != "" {
		cmd = fmt.Sprintf("%s --from-backup=%s", cmd, c.backupPath)
	}
	if err := sess.Start(cmd); err != nil {
		c.uploadDebugInfo(t)
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
			c.uploadDebugInfo(t)
			return fmt.Errorf("bootstrap: %s %s error: %s", step.ID, step.Action, step.Error)
		}
		c.SendLog(fmt.Sprintf("%s: %s", step.ID, step.State))
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

	if err := sess.Wait(); err != nil {
		return err
	}

	if c.backupPath != "" {
		dashboardEnv, err := c.getAppEnv("dashboard", t)
		if err != nil {
			return err
		}
		c.DashboardLoginToken = dashboardEnv["LOGIN_TOKEN"]

		dm, err := c.migrateDomain(&ct.DomainMigration{
			OldDomain: c.oldDomain,
			Domain:    c.Domain.Name,
		}, t)
		if err != nil {
			return err
		}
		if err := c.migrateDomainWorkaroundIssue2987(dm, t); err != nil {
			c.SendLog(fmt.Sprintf("WARNING: Failed to run workaround for issue #2987: %s", err))
		}
		c.CACert = dm.TLSCert.CACert
		c.ControllerPin = dm.TLSCert.Pin
	} else {
		if keyData.Key == "" || controllerCertData.Pin == "" {
			return err
		}

		c.ControllerKey = keyData.Key
		c.ControllerPin = controllerCertData.Pin
		c.CACert = controllerCertData.CACert
		c.DashboardLoginToken = loginTokenData.Token
	}

	if err := c.saveField("ControllerKey", c.ControllerKey); err != nil {
		return err
	}
	if err := c.saveField("ControllerPin", c.ControllerPin); err != nil {
		return err
	}
	if err := c.saveField("CACert", c.CACert); err != nil {
		return err
	}
	if err := c.saveField("DashboardLoginToken", c.DashboardLoginToken); err != nil {
		return err
	}

	return nil
}

func (c *BaseCluster) getAppEnv(appName string, t *TargetServer) (map[string]string, error) {
	c.SendLog(fmt.Sprintf("Getting ENV for %s", appName))

	client, err := cc.NewClientWithHTTP(fmt.Sprintf("http://%s", t.IP), c.ControllerKey, &http.Client{Transport: &http.Transport{Dial: t.SSHClient.Dial}})
	if err != nil {
		return nil, err
	}
	if v1client, ok := client.(*v1controller.Client); ok {
		v1client.Host = fmt.Sprintf("controller.%s", c.oldDomain)
	}

	release, err := client.GetAppRelease(appName)
	if err != nil {
		return nil, err
	}
	return release.Env, nil
}

func (c *BaseCluster) migrateDomain(dm *ct.DomainMigration, t *TargetServer) (*ct.DomainMigration, error) {
	c.SendLog(fmt.Sprintf("Migrating domain (%s to %s)", dm.OldDomain, dm.Domain))

	client, err := cc.NewClientWithHTTP(fmt.Sprintf("http://%s", t.IP), c.ControllerKey, &http.Client{Transport: &http.Transport{Dial: t.SSHClient.Dial}})
	if err != nil {
		return nil, err
	}
	if v1client, ok := client.(*v1controller.Client); ok {
		v1client.Host = fmt.Sprintf("controller.%s", dm.OldDomain)
	}

	events := make(chan *ct.Event)
	stream, err := client.StreamEvents(ct.StreamEventsOptions{
		ObjectTypes: []ct.EventType{ct.EventTypeDomainMigration},
	}, events)
	if err != nil {
		return nil, fmt.Errorf("Error opening domain migration event stream: %s", err)
	}
	defer stream.Close()

	if err := client.PutDomain(dm); err != nil {
		return nil, fmt.Errorf("Error starting domain migration: %s", err)
	}

	timeout := time.After(5 * time.Minute)
	var e *ct.DomainMigrationEvent
	for {
		select {
		case event, ok := <-events:
			if !ok {
				return nil, fmt.Errorf("Error streaming domain migration events: %s", stream.Err())
			}
			if err := json.Unmarshal(event.Data, &e); err != nil {
				return nil, err
			}
			if e.Error != "" {
				return nil, fmt.Errorf("Domain migration error: %s", e.Error)
			}
			if e.DomainMigration.FinishedAt != nil {
				return e.DomainMigration, nil
			}
		case <-timeout:
			return nil, errors.New("timed out waiting for domain migration to complete")
		}
	}
}

// Workaround for https://github.com/flynn/flynn/issues/2987
// Make sure system apps are using correct cert
func (c *BaseCluster) migrateDomainWorkaroundIssue2987(dm *ct.DomainMigration, t *TargetServer) error {
	client, err := cc.NewClientWithHTTP(fmt.Sprintf("http://%s", t.IP), c.ControllerKey, &http.Client{Transport: &http.Transport{Dial: t.SSHClient.Dial}})
	if err != nil {
		return fmt.Errorf("Error creating client: %s", err)
	}
	if v1client, ok := client.(*v1controller.Client); ok {
		v1client.Host = fmt.Sprintf("controller.%s", dm.OldDomain)
	}

	for _, appName := range []string{"controller", "dashboard"} {
		app, err := client.GetApp(appName)
		if err != nil {
			return fmt.Errorf("Error fetching app %s: %s", appName, err)
		}
		routes, err := client.RouteList(app.ID)
		if err != nil {
			return fmt.Errorf("Error listing routes for %s: %s", appName, err)
		}
		var route *router.Route
		for _, r := range routes {
			if strings.HasSuffix(r.Domain, dm.Domain) {
				route = r
				break
			}
		}
		if route == nil {
			return fmt.Errorf("couldn't find route for %s matching %s", appName, dm.Domain)
		}
		route.Certificate = &router.Certificate{
			Cert: dm.TLSCert.Cert,
			Key:  dm.TLSCert.PrivateKey,
		}
		if err := client.UpdateRoute(app.ID, route.FormattedID(), route); err != nil {
			return fmt.Errorf("Error updating route for app %s: %s", appName, err)
		}
	}
	return nil
}

func (c *BaseCluster) waitForDNS() error {
	c.SendLog("Waiting for DNS to propagate")
	for {
		status, err := c.Domain.Status()
		if err != nil {
			return err
		}
		if status == "applied" {
			break
		}
		time.Sleep(time.Second)
	}
	c.SendLog("DNS is live")
	return nil
}

func (c *BaseCluster) configureCLI() error {
	config, err := cfg.ReadFile(cfg.DefaultPath())
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	cluster := c.ClusterConfig()
	if err := config.Add(cluster, true); err != nil {
		return err
	}
	config.SetDefault(c.Name)
	if err := config.SaveTo(cfg.DefaultPath()); err != nil {
		return err
	}

	caFile, err := cfg.CACertFile(cluster.Name)
	if err != nil {
		return err
	}
	defer caFile.Close()
	if _, err := caFile.Write([]byte(c.CACert)); err != nil {
		return err
	}

	if err := cfg.WriteGlobalGitConfig(cluster.GitURL, caFile.Name()); err != nil {
		return err
	}

	c.SendLog("CLI configured locally")
	return nil
}

func (c *BaseCluster) genIPTablesConfigScript() (string, error) {
	var data struct {
		InstanceIPs []string
	}
	data.InstanceIPs = c.InstanceIPs
	var buf bytes.Buffer
	err := iptablesConfigScript.Execute(&buf, data)
	return buf.String(), err
}

func (c *BaseCluster) genStartScript(nodes int64, dataDisk string) (string, string, error) {
	data := struct {
		DiscoveryToken, DataDisk string
	}{DataDisk: dataDisk}
	var err error
	data.DiscoveryToken, err = discovery.NewToken()
	if err != nil {
		return "", "", err
	}
	buf := &bytes.Buffer{}
	w := base64.NewEncoder(base64.StdEncoding, buf)
	err = startScript.Execute(w, data)
	w.Close()

	return buf.String(), data.DiscoveryToken, err
}

var iptablesConfigScript = template.Must(template.New("iptables.sh").Parse(`
export DEBIAN_FRONTEND=noninteractive
apt-get update
apt-get install -y iptables-persistent
iptables -F INPUT
{{ range $i, $ip := .InstanceIPs }}
iptables -A INPUT -s {{$ip}} -j ACCEPT
{{ end }}
iptables -A INPUT -i eth0 -m conntrack --ctstate ESTABLISHED,RELATED -j ACCEPT
iptables -A INPUT -i eth0 -p tcp --dport 80 -j ACCEPT
iptables -A INPUT -i eth0 -p tcp --dport 443 -j ACCEPT
iptables -A INPUT -i eth0 -p tcp --dport 22 -j ACCEPT
iptables -A INPUT -i eth0 -p icmp --icmp-type echo-request -j ACCEPT
iptables -A INPUT -i eth0 -j DROP
netfilter-persistent save
`[1:]))

var startScript = template.Must(template.New("start.sh").Parse(`
#!/bin/bash
set -e -x -o pipefail

FIRST_BOOT="/var/lib/flynn/first-boot"
mkdir -p /var/lib/flynn

if [[ ! -f "${FIRST_BOOT}" ]]; then
  {{if .DataDisk}}

  # if the sparse file zpool exists, replace it with the disk
  vdev="/var/lib/flynn/volumes/zfs/vdev/flynn-default-zpool.vdev"
  if [[ -f "${vdev}" ]]; then
    zpool set autoexpand=on flynn-default
    zpool replace -f flynn-default "${vdev}" {{.DataDisk}}
    while zpool status | grep -q "${vdev}"; do
      sleep 1
    done
    rm "${vdev}"
  else
    zpool create -f -m none flynn-default {{.DataDisk}}
  fi
  {{end}}

  flynn-host init --discovery={{.DiscoveryToken}}

  source /etc/lsb-release
  case "${DISTRIB_RELEASE}" in
    14.04)
      start flynn-host
      sed -i 's/#start on/start on/' /etc/init/flynn-host.conf
      ;;
    16.04)
      systemctl enable flynn-host
      systemctl start flynn-host
      ;;
  esac

  touch "${FIRST_BOOT}"
fi
`[1:]))
