package installer

import (
	"bytes"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"text/template"
	"time"

	"github.com/flynn/flynn/Godeps/_workspace/src/golang.org/x/crypto/ssh"
	cfg "github.com/flynn/flynn/cli/config"
	"github.com/flynn/flynn/pkg/etcdcluster"
)

func (c *Cluster) saveField(field string, value interface{}) error {
	c.installer.dbMtx.Lock()
	defer c.installer.dbMtx.Unlock()

	var err error

	tx, err := c.installer.db.Begin()
	if err != nil {
		return err
	}

	switch field {
	case "InstanceIPs":
		insertStmt := ""
		fields := make([]interface{}, 0, len(c.InstanceIPs)+1)
		fields = append(fields, c.ID)
		for i, ip := range c.InstanceIPs {
			insertStmt += fmt.Sprintf("INSERT INTO instances (ClusterID, IP) VALUES ($1, $%d);\n", i+2)
			fields = append(fields, ip)
		}
		q := fmt.Sprintf(`
    %s
    `, insertStmt)
		_, err = tx.Exec(q, fields...)
	case "Domain":
		_, err = tx.Exec(`
    INSERT INTO domains (ClusterID, Name, Token) VALUES ($1, $2, $3);
    `, c.ID, c.Domain.Name, c.Domain.Token)
	default:
		_, err = tx.Exec(fmt.Sprintf(`
    UPDATE clusters SET %s = $2 WHERE ID == $1;
    `, field), c.ID, value)
	}

	if err != nil {
		tx.Rollback()
		return err
	}
	return tx.Commit()
}

func (c *Cluster) setState(state string) {
	c.State = state
	if err := c.saveField("State", state); err != nil {
		c.installer.logger.Debug(fmt.Sprintf("Error saving cluster State: %s", err.Error()))
	}
	c.sendEvent(&Event{
		ClusterID:   c.ID,
		Type:        "cluster_state",
		Description: state,
	})
}

func (c *Cluster) RemoveFromDB() (err error) {
	var tx *sql.Tx
	tx, err = c.installer.db.Begin()
	if err != nil {
		return
	}

	defer func() {
		if err != nil {
			tx.Rollback()
			return
		}
		err = tx.Commit()
	}()

	if _, err = tx.Exec(`DELETE FROM prompts WHERE ID IN (SELECT PromptID FROM events WHERE ClusterID == $1)`, c.ID); err != nil {
		return
	}

	if _, err = tx.Exec(`DELETE FROM events WHERE ClusterID == $1`, c.ID); err != nil {
		return
	}

	if _, err = tx.Exec(`DELETE FROM domains WHERE ClusterID == $1`, c.ID); err != nil {
		return
	}

	if _, err = tx.Exec(`DELETE FROM instances WHERE ClusterID == $1`, c.ID); err != nil {
		return
	}

	if _, err = tx.Exec(`DELETE FROM clusters WHERE ID == $1`, c.ID); err != nil {
		return
	}

	if _, err = tx.Exec(`DELETE FROM aws_clusters WHERE ClusterID == $1`, c.ID); err != nil {
		return
	}
	c.installer.ClusterDeleted(c.ID)
	return
}

func (c *Cluster) SetDefaultsAndValidate() error {
	if c.NumInstances == 0 {
		c.NumInstances = 1
	}
	c.InstanceIPs = make([]string, 0, c.NumInstances)
	return c.validateInputs()
}

func (c *Cluster) validateInputs() error {
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

func (c *Cluster) StackAddCmd() (string, error) {
	if c.ControllerKey == "" || c.ControllerPin == "" || c.Domain == nil || c.Domain.Name == "" {
		return "", fmt.Errorf("Not enough data present")
	}
	return fmt.Sprintf("flynn cluster add -g %[1]s:2222 -p %[2]s default https://controller.%[1]s %[3]s", c.Domain.Name, c.ControllerPin, c.ControllerKey), nil
}

func (c *Cluster) ClusterConfig() *cfg.Cluster {
	return &cfg.Cluster{
		Name:    c.Name,
		URL:     "https://controller." + c.Domain.Name,
		Key:     c.ControllerKey,
		GitHost: fmt.Sprintf("%s:2222", c.Domain.Name),
		TLSPin:  c.ControllerPin,
	}
}

func (c *Cluster) DashboardLoginMsg() (string, error) {
	if c.DashboardLoginToken == "" || c.Domain == nil || c.Domain.Name == "" {
		return "", fmt.Errorf("Not enough data present")
	}
	return fmt.Sprintf("The built-in dashboard can be accessed at http://dashboard.%s with login token %s", c.Domain.Name, c.DashboardLoginToken), nil
}

func (c *Cluster) allocateDomain() error {
	c.SendInstallLogEvent("Allocating domain")
	domain, err := AllocateDomain()
	if err != nil {
		return err
	}
	domain.ClusterID = c.ID
	c.Domain = domain
	return c.saveField("Domain", domain)
}

func instanceRunCmd(cmd string, sshConfig *ssh.ClientConfig, ipAddress string) (stdout, stderr io.Reader, err error) {
	var sshConn *ssh.Client
	sshConn, err = ssh.Dial("tcp", ipAddress+":22", sshConfig)
	if err != nil {
		return
	}
	defer sshConn.Close()

	sess, err := sshConn.NewSession()
	if err != nil {
		return
	}
	stdout, err = sess.StdoutPipe()
	if err != nil {
		return
	}
	stderr, err = sess.StderrPipe()
	if err != nil {
		return
	}
	if err = sess.Start(cmd); err != nil {
		return
	}

	err = sess.Wait()
	return
}

func (c *Cluster) uploadDebugInfo(sshConfig *ssh.ClientConfig, ipAddress string) {
	cmd := "sudo flynn-host collect-debug-info"
	stdout, stderr, _ := instanceRunCmd(cmd, sshConfig, ipAddress)
	var buf bytes.Buffer
	io.Copy(&buf, stdout)
	io.Copy(&buf, stderr)
	c.SendInstallLogEvent(fmt.Sprintf("`%s` output for %s: %s", cmd, ipAddress, buf.String()))
}

type stepInfo struct {
	ID        string           `json:"id"`
	Action    string           `json:"action"`
	Data      *json.RawMessage `json:"data"`
	State     string           `json:"state"`
	Error     string           `json:"error,omitempty"`
	Timestamp time.Time        `json:"ts"`
}

func (c *Cluster) bootstrap() error {
	c.SendInstallLogEvent("Running bootstrap")

	if c.SSHKey == nil {
		return errors.New("No SSHKey found")
	}

	// bootstrap only needs to run on one instance
	ipAddress := c.InstanceIPs[0]

	signer, err := ssh.NewSignerFromKey(c.SSHKey.PrivateKey)
	if err != nil {
		return err
	}
	sshConfig := &ssh.ClientConfig{
		User: "ubuntu",
		Auth: []ssh.AuthMethod{ssh.PublicKeys(signer)},
	}

	attempts := 0
	maxAttempts := 3
	var sshConn *ssh.Client
	for {
		sshConn, err = ssh.Dial("tcp", ipAddress+":22", sshConfig)
		if err != nil {
			if attempts < maxAttempts {
				attempts += 1
				time.Sleep(time.Second)
				continue
			}
			return err
		}
		break
	}
	defer sshConn.Close()

	sess, err := sshConn.NewSession()
	if err != nil {
		return err
	}
	stdout, err := sess.StdoutPipe()
	if err != nil {
		return err
	}
	sess.Stderr = os.Stderr
	if err := sess.Start(fmt.Sprintf("CLUSTER_DOMAIN=%s flynn-host bootstrap --json", c.Domain.Name)); err != nil {
		c.uploadDebugInfo(sshConfig, ipAddress)
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
			c.uploadDebugInfo(sshConfig, ipAddress)
			return fmt.Errorf("bootstrap: %s %s error: %s", step.ID, step.Action, step.Error)
		}
		c.SendInstallLogEvent(fmt.Sprintf("%s: %s", step.ID, step.State))
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
	if err := c.waitForDNS(); err != nil {
		return err
	}

	return nil
}

func (c *Cluster) waitForDNS() error {
	c.SendInstallLogEvent("Waiting for DNS to propagate")
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
	c.SendInstallLogEvent("DNS is live")
	return nil
}

func (c *Cluster) configureCLI() error {
	config, err := cfg.ReadFile(cfg.DefaultPath())
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	if err := config.Add(c.ClusterConfig(), true); err != nil {
		return err
	}
	config.SetDefault(c.Name)
	if err := config.SaveTo(cfg.DefaultPath()); err != nil {
		return err
	}
	c.SendInstallLogEvent("CLI configured locally")
	return nil
}

func (c *Cluster) genStartScript(nodes int64) (string, string, error) {
	var data struct {
		DiscoveryToken string
	}
	var err error
	data.DiscoveryToken, err = etcdcluster.NewDiscoveryToken(strconv.FormatInt(nodes, 10))
	if err != nil {
		return "", "", err
	}
	buf := &bytes.Buffer{}
	w := base64.NewEncoder(base64.StdEncoding, buf)
	err = startScript.Execute(w, data)
	w.Close()

	return buf.String(), data.DiscoveryToken, err
}

var startScript = template.Must(template.New("start.sh").Parse(`
#!/bin/sh

# wait for libvirt
while ! [ -e /var/run/libvirt/libvirt-sock ]; do
  sleep 0.1
done

flynn-host init --discovery={{.DiscoveryToken}}
start flynn-host
`[1:]))
