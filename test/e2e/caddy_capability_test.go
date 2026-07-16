//go:build e2e

package e2e

import (
	"os/exec"
)

func init() {
	caddyPath, err := exec.LookPath("caddy")
	if err != nil {
		return
	}
	// The production veil-caddy unit grants CAP_NET_BIND_SERVICE. Mirror that
	// execution boundary so Caddy can create its automatic HTTP redirect
	// listener while the Naive data path is validated over HTTPS.
	_ = exec.Command("sudo", "setcap", "cap_net_bind_service=+ep", caddyPath).Run()
}
