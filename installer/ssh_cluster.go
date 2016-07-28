package installer

import (
	"bytes"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"io/ioutil"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"github.com/flynn/flynn/cli/config"
	"github.com/flynn/flynn/pkg/httphelper"
	"github.com/flynn/flynn/pkg/knownhosts"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
)

func (c *SSHCluster) Type() string {
	return "ssh"
}

func (c *SSHCluster) Base() *BaseCluster {
	return c.base
}

func (c *SSHCluster) SetBase(base *BaseCluster) {
	c.base = base
}

func (c *SSHCluster) SetCreds(creds *Credential) error {
	return nil
}

func (c *SSHCluster) SetDefaultsAndValidate() error {
	c.ClusterID = c.base.ID

	data, err := json.Marshal(c.Targets)
	if err != nil {
		return err
	}
	c.TargetsJSON = string(data)

	if err := c.validateInputs(); err != nil {
		return err
	}

	c.base.SSHUsername = c.SSHLogin

	if err := c.base.SetDefaultsAndValidate(); err != nil {
		return err
	}

	return nil
}

func (c *SSHCluster) validateInputs() error {
	for _, t := range c.Targets {
		if net.ParseIP(t.IP) == nil {
			return httphelper.JSONError{
				Code:    httphelper.ValidationErrorCode,
				Message: fmt.Sprintf("%s is not a valid IP address", t.IP),
			}
		}
		if t.User == "" {
			return httphelper.JSONError{
				Code:    httphelper.ValidationErrorCode,
				Message: fmt.Sprintf("No user given for %s", t.IP),
			}
		}
		if t.Port == "" {
			return httphelper.JSONError{
				Code:    httphelper.ValidationErrorCode,
				Message: fmt.Sprintf("No port given for %s", t.IP),
			}
		}
	}
	return nil
}

func (c *SSHCluster) Run() {
	go func() {
		defer c.base.handleDone()
		defer func() {
			// clear cached passwords
			c.base.passwordPromptMtx.Lock()
			c.base.passwordCache = make(map[string]string)
			c.base.passwordPromptMtx.Unlock()
		}()

		steps := []func() error{
			c.findSSHAuth,
			c.saveInstanceIPs,
			c.base.allocateDomain,
			c.configureDNS,
			c.uploadBackup,
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

func (c *SSHCluster) sshAgent() agent.Agent {
	if sock, err := net.Dial("unix", os.Getenv("SSH_AUTH_SOCK")); err == nil {
		return agent.NewClient(sock)
	}
	return nil
}

type privateKeySigner struct {
	Encrypted bool

	pem       *pem.Block
	key       *rsa.PrivateKey
	publicKey ssh.PublicKey
	path      string

	base *BaseCluster
}

func (p privateKeySigner) Passphrase() string {
	return p.base.PromptProtectedInput(fmt.Sprintf("Please enter the passphrase to decrypt the key: %s", p.path))
}

func (p privateKeySigner) Decrypt() (ssh.Signer, error) {
	if p.key == nil {
		pem, err := x509.DecryptPEMBlock(p.pem, []byte(p.Passphrase()))
		if err != nil {
			return nil, err
		}
		p.key, err = x509.ParsePKCS1PrivateKey(pem)
		if err != nil {
			return nil, err
		}
		p.Encrypted = false
	}
	return ssh.NewSignerFromKey(p.key)
}

func (p privateKeySigner) PublicKey() ssh.PublicKey {
	return p.publicKey
}

func (p privateKeySigner) Sign(rand io.Reader, data []byte) (*ssh.Signature, error) {
	signer, err := p.Decrypt()
	if err != nil {
		fmt.Println(err)
		return nil, err
	}
	return signer.Sign(rand, data)
}

// sort []privateKeySigner with unencrypted keys first,
// and encrypted keys with public keys before those without
type decryptedFirst []privateKeySigner

func (s decryptedFirst) Len() int      { return len(s) }
func (s decryptedFirst) Swap(i, j int) { s[i], s[j] = s[j], s[i] }
func (s decryptedFirst) Less(i, j int) bool {
	if !s[i].Encrypted && s[j].Encrypted {
		return true
	}
	if s[i].Encrypted && !s[j].Encrypted {
		return false
	}
	return s[i].publicKey != nil && s[j].publicKey == nil
}

func (c *SSHCluster) findSSHKeySigners() (signers []privateKeySigner) {
	keyDir := filepath.Join(c.sshDir())
	if stat, err := os.Stat(keyDir); err != nil || !stat.IsDir() {
		return
	}
	walkFunc := func(path string, info os.FileInfo, err error) error {
		if info.IsDir() {
			return nil
		}
		if strings.HasSuffix(path, ".pub") {
			return nil
		}
		data, err := ioutil.ReadFile(path)
		if err != nil {
			return nil
		}
		b, _ := pem.Decode(data)
		if b == nil {
			return nil
		}
		s := privateKeySigner{
			base:      c.base,
			path:      path,
			pem:       b,
			Encrypted: x509.IsEncryptedPEMBlock(b),
		}
		if s.Encrypted {
			publicKeyPath := fmt.Sprintf("%s.pub", path)
			if stat, err := os.Stat(publicKeyPath); err == nil && !stat.IsDir() {
				if data, err := ioutil.ReadFile(publicKeyPath); err == nil {
					pk, _, _, _, err := ssh.ParseAuthorizedKey(data)
					if err == nil {
						s.publicKey = pk
					}
				}
			}
			signers = append(signers, s)
			return nil
		}
		privateKey, err := x509.ParsePKCS1PrivateKey(b.Bytes)
		if err != nil {
			return nil
		}
		s.key = privateKey
		signers = append(signers, s)
		return nil
	}
	filepath.Walk(keyDir, walkFunc)
	sort.Sort(decryptedFirst(signers))
	return
}

func (c *SSHCluster) knownHostsPath() string {
	return filepath.Join(c.sshDir(), "known_hosts")
}

func (c *SSHCluster) sshDir() string {
	if runtime.GOOS == "windows" {
		return filepath.Join(os.Getenv("USERPROFILE"), ".ssh")
	}
	return filepath.Join(config.HomeDir(), ".ssh")
}

func (c *SSHCluster) parseKnownHosts() error {
	file, err := os.Open(c.knownHostsPath())
	if err != nil {
		c.knownHosts = &knownhosts.KnownHosts{}
		return nil
	}
	defer file.Close()
	k, err := knownhosts.Unmarshal(file)
	if err != nil {
		return err
	}
	c.knownHosts = k
	return nil
}

func (c *SSHCluster) fingerprintSSHKey(publicKey ssh.PublicKey) string {
	hash := sha256.New()
	hash.Write(publicKey.Marshal())
	return fmt.Sprintf("SHA256:%s", base64.StdEncoding.EncodeToString(hash.Sum(nil)))
}

func (c *SSHCluster) hostKeyCallback(hostname string, remote net.Addr, key ssh.PublicKey) error {
	if c.knownHosts == nil {
		if err := c.parseKnownHosts(); err != nil {
			return err
		}
	}
	err := c.knownHosts.HostKeyCallback(hostname, remote, key)
	if err == knownhosts.HostNotFoundError {
		fingerprint := c.fingerprintSSHKey(key)
		hostnameFormatted := hostname
		remoteAddr := remote.String()
		if remoteAddr != hostname {
			hostnameFormatted = fmt.Sprintf("%s (%s)", hostname, remoteAddr)
		}
		if c.base.YesNoPrompt(fmt.Sprintf("The authenticity of host '%s' can't be established.\n%s key fingerprint is %s.\nAre you sure you want to continue connecting?", hostnameFormatted, key.Type(), fingerprint)) {
			c.base.SendLog(fmt.Sprintf("Trusting '%s' with key fingerprint %s", hostnameFormatted, fingerprint))
			file, err := os.OpenFile(c.knownHostsPath(), os.O_CREATE|os.O_WRONLY|os.O_APPEND, os.FileMode(0644))
			if err != nil {
				c.base.SendLog(fmt.Sprintf("WARNING: Can't write to %s: %s", c.knownHostsPath(), err))
			} else {
				defer file.Close()
			}
			c.knownHosts.AppendHost(hostname, key, file)
			return nil
		}
	}
	return err
}

func (c *SSHCluster) sshConfigForAuth(t *TargetServer, auth []ssh.AuthMethod) *ssh.ClientConfig {
	return &ssh.ClientConfig{
		HostKeyCallback: c.hostKeyCallback,
		User:            t.User,
		Auth:            auth,
	}
}

func (c *SSHCluster) testAndAddAuthentication(target *TargetServer, sshConfig *ssh.ClientConfig) bool {
	conn, err := ssh.Dial("tcp", fmt.Sprintf("%s:%s", target.IP, target.Port), sshConfig)
	if err != nil {
		return false
	}
	target.SSHConfig = sshConfig
	target.SSHClient = conn
	return true
}

func (c *SSHCluster) findSSHAuth() error {
	c.base.SendLog("Detecting authentication")

	testAndAddAuthMethod := func(t *TargetServer, a ssh.AuthMethod) bool {
		sshConfig := c.sshConfigForAuth(t, []ssh.AuthMethod{a})
		if c.testAndAddAuthentication(t, sshConfig) {
			c.base.SendLog(fmt.Sprintf("Verified authentication for %s@%s", t.User, t.IP))
			return true
		}
		return false
	}

	testAndAddSigner := func(t *TargetServer, s ssh.Signer) bool {
		return testAndAddAuthMethod(t, ssh.PublicKeys(s))
	}

	testAllAuthenticated := func(targets []*TargetServer) bool {
		for _, t := range targets {
			if t.SSHConfig == nil {
				return false
			}
		}
		return true
	}

	sshAgent := c.sshAgent()
	if sshAgent != nil {
		sshAgentAuth := ssh.PublicKeysCallback(sshAgent.Signers)
		for _, t := range c.Targets {
			testAndAddAuthMethod(t, sshAgentAuth)
		}
	}

	if testAllAuthenticated(c.Targets) {
		return nil
	}

	var agentKeys [][]byte
	if sshAgent != nil {
		if keys, err := sshAgent.List(); err == nil {
			agentKeys = make([][]byte, len(keys))
			for i, k := range keys {
				agentKeys[i] = k.Marshal()
			}
		}
	}

	var signers []privateKeySigner

signerloop:
	for _, s := range c.findSSHKeySigners() {
		if s.publicKey != nil {
			for _, k := range agentKeys {
				if bytes.Equal(k, s.publicKey.Marshal()) {
					continue signerloop
				}
			}
		}
		signers = append(signers, s)
	}

outer:
	for _, t := range c.Targets {
		if t.SSHConfig != nil {
			continue
		}
		for _, s := range signers {
			if s.Encrypted {
				if s.publicKey == nil {
					signer, err := s.Decrypt()
					if err != nil {
						continue
					}
					if testAndAddSigner(t, signer) {
						continue outer
					}
				} else {
					if testAndAddSigner(t, s) {
						continue outer
					}
				}
			} else {
				signer, err := ssh.NewSignerFromKey(s.key)
				if err != nil {
					continue
				}
				if testAndAddSigner(t, signer) {
					continue outer
				}
			}
		}
	}

	for _, t := range c.Targets {
		if t.SSHConfig != nil {
			continue
		}
		answer, err := c.base.ChoicePrompt(Choice{
			Message: "No working authentication found.\nPlease choose one of the following options:",
			Options: []ChoiceOption{
				{
					Type:  1,
					Name:  "Private key",
					Value: "1",
				},
				{
					Name:  "Password",
					Value: "2",
				},
				{
					Name:  "Abort",
					Value: "3",
				},
			},
		})
		if err != nil {
			return err
		}
		switch answer {
		case "1":
			if err := c.importSSHKeyPair(t); err != nil {
				return err
			} else {
				continue
			}
		case "2":
			password := c.base.PromptProtectedInput(fmt.Sprintf("Please enter your password for %s@%s", t.User, t.IP))
			if testAndAddAuthMethod(t, ssh.Password(password)) {
				continue
			}
		}
		return fmt.Errorf("No working authentication found")
	}
	return nil
}

func (c *SSHCluster) importSSHKeyPair(t *TargetServer) error {
	var buf bytes.Buffer
	_, file, readFileErrChan := c.base.PromptFileInput(fmt.Sprintf("Please provide your private key for %s@%s", t.User, t.IP))
	if _, err := io.Copy(&buf, file); err != nil {
		readFileErrChan <- err
		return err
	}
	readFileErrChan <- nil // no error reading file
	b, _ := pem.Decode(buf.Bytes())
	if b == nil {
		return fmt.Errorf("Invalid private key")
	}
	var pemBytes []byte
	if x509.IsEncryptedPEMBlock(b) {
		passphrase := c.base.PromptProtectedInput("Please enter the passphrase for the key")
		var err error
		pemBytes, err = x509.DecryptPEMBlock(b, []byte(passphrase))
		if err != nil {
			return err
		}
	} else {
		pemBytes = b.Bytes
	}
	privateKey, err := x509.ParsePKCS1PrivateKey(pemBytes)
	if err != nil {
		return err
	}

	signer, err := ssh.NewSignerFromKey(privateKey)
	if err != nil {
		return err
	}
	auth := []ssh.AuthMethod{ssh.PublicKeys(signer)}

	sshConfig := c.sshConfigForAuth(t, auth)

	c.base.SendLog(fmt.Sprintf("Testing provided key for %s@%s", t.User, t.IP))
	if !c.testAndAddAuthentication(t, sshConfig) {
		return fmt.Errorf("Provided key for %s@%s failed to authenticate", t.User, t.IP)
	}
	c.base.SendLog(fmt.Sprintf("Key verified for %s@%s", t.User, t.IP))
	return nil
}

func (c *SSHCluster) saveInstanceIPs() error {
	ips := make([]string, len(c.Targets))
	for i, t := range c.Targets {
		ips[i] = t.IP
	}
	c.base.InstanceIPs = ips
	return c.base.saveInstanceIPs()
}

func (c *SSHCluster) configureDNS() error {
	c.base.SendLog("Configuring DNS")
	return c.base.Domain.ConfigureIPAddresses(c.base.InstanceIPs)
}

func (c *SSHCluster) installFlynn() error {
	bareCluster := &BareCluster{
		Base:    c.base,
		Targets: c.Targets,
	}
	return bareCluster.InstallFlynn()
}

func (c *SSHCluster) uploadBackup() error {
	// upload backup to bootstrap instance
	return c.base.uploadBackupToTargetWithRetry(c.Targets[0])
}

func (c *SSHCluster) bootstrap() error {
	// bootstrap only needs to run on one instance
	return c.base.bootstrapTarget(c.Targets[0])
}

func (c *SSHCluster) Delete() {
	c.base.setState("deleting")
	if err := c.base.MarkDeleted(); err != nil {
		c.base.SendError(err)
	}
	c.base.sendEvent(&Event{
		ClusterID:   c.base.ID,
		Type:        "cluster_state",
		Description: "deleted",
	})
}
