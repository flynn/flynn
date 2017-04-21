package installer

import (
	"fmt"
	"time"
)

type BareCluster struct {
	Targets []*TargetServer
	Base    *BaseCluster

	startScript string
}

func (c *BareCluster) InstallFlynn() error {
	c.Base.SendLog("Installing flynn")

	var err error
	c.startScript, c.Base.DiscoveryToken, err = c.Base.genStartScript(c.Base.NumInstances, "")
	if err != nil {
		return err
	}
	if err := c.Base.saveField("DiscoveryToken", c.Base.DiscoveryToken); err != nil {
		return err
	}

	errChan := make(chan error, len(c.Targets))
	ops := []func(*TargetServer) error{
		c.instanceInstallFlynn,
		c.instanceStartFlynn,
	}
	for _, op := range ops {
		for _, t := range c.Targets {
			go func(t *TargetServer) {
				errChan <- op(t)
			}(t)
		}
		for range c.Targets {
			if err := <-errChan; err != nil {
				return err
			}
		}
	}
	return nil
}

func (c *BareCluster) instanceInstallFlynn(t *TargetServer) error {
	attemptsRemaining := 3
	var err error
	for {
		c.Base.SendLog(fmt.Sprintf("Installing flynn on %s", t.IP))
		cmd := "curl -fsSL -o /tmp/install-flynn https://dl.flynn.io/install-flynn && sudo bash /tmp/install-flynn"
		if c.Base.ReleaseChannel != "" {
			cmd = fmt.Sprintf("%s --channel %s", cmd, c.Base.ReleaseChannel)
		}
		if c.Base.ReleaseVersion != "" {
			cmd = fmt.Sprintf("%s --version %s", cmd, c.Base.ReleaseVersion)
		}
		if c.Base.Type == "ssh" {
			cmd = cmd + " --clean"
		}
		if t.SSHClient == nil {
			err = c.Base.instanceRunCmd(cmd, t.SSHConfig, t.IP)
		} else {
			err = c.Base.instanceRunCmdWithClient(cmd, t.SSHClient, t.User, t.IP)
		}
		if c.Base.IsAborted() {
			return fmt.Errorf("Install aborted")
		}
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

func (c *BareCluster) instanceStartFlynn(t *TargetServer) error {
	c.Base.SendLog(fmt.Sprintf("Starting flynn on %s", t.IP))
	cmd := fmt.Sprintf(`echo "%s" | base64 -d > /tmp/start-flynn && sudo bash /tmp/start-flynn`, c.startScript)
	if t.SSHClient == nil {
		return c.Base.instanceRunCmd(cmd, t.SSHConfig, t.IP)
	}
	return c.Base.instanceRunCmdWithClient(cmd, t.SSHClient, t.User, t.IP)
}
