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
	"os"
	"strings"
	"text/template"
	"time"

	"github.com/flynn/flynn/Godeps/_workspace/src/golang.org/x/crypto/ssh"
	"github.com/flynn/flynn/bootstrap/discovery"
	cfg "github.com/flynn/flynn/cli/config"
)

func (c *BaseCluster) FindCredentials() (*Credential, error) {
	return c.installer.FindCredentials(c.CredentialID)
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
	c.State = state
	if err := c.saveField("State", state); err != nil {
		c.installer.logger.Debug(fmt.Sprintf("Error saving cluster State: %s", err.Error()))
	}
	if c.State == "running" {
		if err := c.installer.txExec(`
			UPDATE events SET DeletedAt = now() WHERE Type == "log" AND ClusterID == $1;
		`, c.ID); err != nil {
			c.installer.logger.Debug(fmt.Sprintf("Error updating events: %s", err.Error()))
		}
		c.installer.removeClusterLogEvents(c.ID)
	}
	c.sendEvent(&Event{
		ClusterID:   c.ID,
		Type:        "cluster_state",
		Description: state,
	})
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
		for scanner.Scan() {
			select {
			case _, _ = <-doneChan:
				return
			default:
			}

			c.SendLog(scanner.Text())
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
		target.SSHClient.Close()
	}()

	return c.bootstrapTarget(target)
}

func (c *BaseCluster) bootstrapTarget(t *TargetServer) error {
	c.SendLog("Running bootstrap")

	if t.SSHClient == nil {
		attempts := 0
		maxAttempts := 3
		for {
			var err error
			t.SSHClient, err = ssh.Dial("tcp", net.JoinHostPort(t.IP, t.Port), t.SSHConfig)
			if err != nil {
				if attempts < maxAttempts {
					attempts += 1
					c.SendLog(err.Error())
					time.Sleep(time.Second)
					continue
				}
				return err
			}
			break
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
	if err := sess.Start(fmt.Sprintf("CLUSTER_DOMAIN=%s flynn-host bootstrap --min-hosts=%d --discovery=%s --json", c.Domain.Name, c.NumInstances, c.DiscoveryToken)); err != nil {
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
	if keyData.Key == "" || controllerCertData.Pin == "" {
		return err
	}

	c.ControllerKey = keyData.Key
	c.ControllerPin = controllerCertData.Pin
	c.CACert = controllerCertData.CACert
	c.DashboardLoginToken = loginTokenData.Token

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

	if err := sess.Wait(); err != nil {
		return err
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

func (c *BaseCluster) genStartScript(nodes int64) (string, string, error) {
	var data struct {
		DiscoveryToken string
	}
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
apt-get install -y iptables-persistent
{{ range $i, $ip := .InstanceIPs }}
iptables -A FORWARD -s {{$ip}} -j ACCEPT
{{ end }}
iptables -A FORWARD -i eth0 -m conntrack --ctstate ESTABLISHED,RELATED -j ACCEPT
iptables -A FORWARD -i eth0 -p tcp --dport 80 -j ACCEPT
iptables -A FORWARD -i eth0 -p tcp --dport 443 -j ACCEPT
iptables -A FORWARD -i eth0 -p tcp --dport 22 -j ACCEPT
iptables -A FORWARD -i eth0 -p icmp --icmp-type echo-request -j ACCEPT
iptables -A FORWARD -i eth0 -j DROP
/etc/init.d/iptables-persistent save
`[1:]))

var startScript = template.Must(template.New("start.sh").Parse(`
#!/bin/sh

# wait for libvirt
while ! [ -e /var/run/libvirt/libvirt-sock ]; do
  sleep 0.1
done

flynn-host init --discovery={{.DiscoveryToken}}
start flynn-host
`[1:]))
