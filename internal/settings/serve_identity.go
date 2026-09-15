package settings

import (
	"errors"
	"fmt"
	"strings"

	"github.com/mikkelchokolate/Veil/internal/webbasepath"
)

// CheckProcessCanAdoptCaddyIdentity rejects Caddy-facing serve identity
// changes that the running Panel process cannot adopt. Apply reloads Caddy
// from settings but the mux mount, listen address, and Panel access mode
// come from veil.env / `veil serve` flags until repair rewrites them.
func CheckProcessCanAdoptCaddyIdentity(candidate, process Settings) error {
	processPath, err := webbasepath.NormalizeOptional(process.WebBasePath)
	if err != nil {
		return fmt.Errorf("webBasePath: %w", err)
	}
	if processPath == "" {
		return nil
	}
	if !usesCaddyAccess(candidate.PanelAccess) && !usesCaddyAccess(process.PanelAccess) {
		return nil
	}
	candidatePath, err := webbasepath.NormalizeOptional(candidate.WebBasePath)
	if err != nil {
		return fmt.Errorf("webBasePath: %w", err)
	}
	if candidatePath != processPath {
		return errors.New("webBasePath cannot change until veil repair rewrites VEIL_WEB_BASE_PATH and restarts the Panel")
	}
	if access := strings.TrimSpace(process.PanelAccess); access != "" && candidate.PanelAccess != access {
		return errors.New("panelAccess cannot change until veil repair rewrites veil.env and restarts the Panel")
	}
	if listen := strings.TrimSpace(process.PanelListen); listen != "" && candidate.PanelListen != listen {
		return errors.New("panelListen cannot change until veil repair rewrites veil.env and restarts the Panel")
	}
	return nil
}

func usesCaddyAccess(value string) bool {
	return strings.EqualFold(strings.TrimSpace(value), "caddy")
}
