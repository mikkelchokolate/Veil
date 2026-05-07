package api

import (
	"context"
	"os/exec"
	"strings"
	"time"
)

var firewallStatusReader = readFirewallStatus

func readFirewallStatus() (bool, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	output, err := exec.CommandContext(ctx, "ufw", "status").CombinedOutput()
	if err != nil {
		return false, nil
	}
	return strings.Contains(string(output), "Status: active"), nil
}
