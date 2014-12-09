package iptables

// This package is originally from Docker and has been modified for use by the
// Flynn project. See the NOTICE and LICENSE files for licensing and copyright
// details.

import (
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

var (
	ErrIptablesNotFound = errors.New("Iptables not found")
	supportsXlock       = false
)

type ChainError struct {
	Chain  string
	Output []byte
}

func (e *ChainError) Error() string {
	return fmt.Sprintf("iptables: %s: %s", e.Chain, string(e.Output))
}

func init() {
	supportsXlock = exec.Command("iptables", "--wait", "-L", "-n").Run() == nil
}

func EnableOutboundNAT(bridge, network string) error {
	natArgs := []string{"POSTROUTING", "-t", "nat", "-s", network, "!", "-o", bridge, "-j", "MASQUERADE"}
	if !Exists(natArgs...) {
		if output, err := Raw(append([]string{"-I"}, natArgs...)...); err != nil {
			return fmt.Errorf("Unable to enable network bridge NAT: %s", err)
		} else if len(output) != 0 {
			return &ChainError{Chain: "POSTROUTING", Output: output}
		}
	}

	// Accept all non-intercontainer outgoing packets
	outgoingArgs := []string{"FORWARD", "-i", bridge, "!", "-o", bridge, "-j", "ACCEPT"}
	if !Exists(outgoingArgs...) {
		if output, err := Raw(append([]string{"-I"}, outgoingArgs...)...); err != nil {
			return fmt.Errorf("Unable to allow outgoing packets: %s", err)
		} else if len(output) != 0 {
			return &ChainError{Chain: "FORWARD outgoing", Output: output}
		}
	}

	// Accept incoming packets for existing connections
	existingArgs := []string{"FORWARD", "-o", bridge, "-m", "conntrack", "--ctstate", "RELATED,ESTABLISHED", "-j", "ACCEPT"}
	if !Exists(existingArgs...) {
		if output, err := Raw(append([]string{"-I"}, existingArgs...)...); err != nil {
			return fmt.Errorf("Unable to allow incoming packets: %s", err)
		} else if len(output) != 0 {
			return &ChainError{Chain: "FORWARD incoming", Output: output}
		}
	}

	return nil
}

// Check if an existing rule exists
func Exists(args ...string) bool {
	if _, err := Raw(append([]string{"-C"}, args...)...); err != nil {
		return false
	}
	return true
}

func Raw(args ...string) ([]byte, error) {
	path, err := exec.LookPath("iptables")
	if err != nil {
		return nil, ErrIptablesNotFound
	}

	if supportsXlock {
		args = append([]string{"--wait"}, args...)
	}

	output, err := exec.Command(path, args...).CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("iptables failed: iptables %v: %s (%s)", strings.Join(args, " "), output, err)
	}

	// ignore iptables' message about xtables lock
	if strings.Contains(string(output), "waiting for it to exit") {
		output = []byte("")
	}

	return output, err
}
