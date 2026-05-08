package installer

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"

	"github.com/veil-panel/veil/internal/renderer"
)

type ManagedFileRepair struct {
	profile RURecommendedProfile
	paths   ApplyPaths
}

func NewManagedFileRepair(profile RURecommendedProfile, paths ApplyPaths) ManagedFileRepair {
	return ManagedFileRepair{profile: profile, paths: paths}
}

func (r ManagedFileRepair) Plan() (RepairPlan, error) {
	files, err := r.desiredFiles()
	if err != nil {
		return RepairPlan{}, err
	}
	plan := RepairPlan{}
	for _, file := range files {
		body, err := os.ReadFile(file.Path)
		if err != nil {
			if isMissingOrBlocked(err) {
				plan.Actions = append(plan.Actions, RepairAction{Path: file.Path, Reason: RepairReasonMissing, Content: file.Content, Mode: file.Mode})
				continue
			}
			return RepairPlan{}, err
		}
		if string(body) != file.Content {
			plan.Actions = append(plan.Actions, RepairAction{Path: file.Path, Reason: RepairReasonDrifted, Content: file.Content, Mode: file.Mode})
		}
	}
	return plan, nil
}

func (r ManagedFileRepair) Apply(plan RepairPlan) (RepairResult, error) {
	result := RepairResult{}
	for _, action := range plan.Actions {
		if err := writeManagedFile(action.Path, action.Content, action.Mode); err != nil {
			return RepairResult{}, err
		}
		result.WrittenFiles = append(result.WrittenFiles, action.Path)
	}
	return result, nil
}

func (r ManagedFileRepair) desiredFiles() ([]managedFile, error) {
	paths := r.paths
	profile := r.profile
	if paths.EtcDir == "" {
		return nil, fmt.Errorf("etc dir is required")
	}
	if paths.VarDir == "" {
		return nil, fmt.Errorf("var dir is required")
	}
	files := []managedFile{}
	if profile.InstallNaive || profile.InstallPanelCaddy {
		files = append(files, managedFile{Path: filepath.Join(paths.EtcDir, "generated", "caddy", "Caddyfile"), Content: profile.Caddyfile, Mode: 0o600})
		files = append(files, managedFile{Path: filepath.Join(paths.VarDir, "www", "index.html"), Content: fallbackIndexHTML(profile.Domain), Mode: 0o644})
	}
	if profile.InstallHysteria2 {
		files = append(files, managedFile{Path: filepath.Join(paths.EtcDir, "generated", "hysteria2", "server.yaml"), Content: profile.Hysteria2YAML, Mode: 0o600})
	}
	panelTLSCertPath := filepath.Join(paths.EtcDir, "panel", "tls.crt")
	panelTLSKeyPath := filepath.Join(paths.EtcDir, "panel", "tls.key")
	if profile.PanelTLSEnabled {
		files = append(files,
			managedFile{Path: panelTLSCertPath, Content: profile.PanelTLSCertPEM, Mode: 0o644},
			managedFile{Path: panelTLSKeyPath, Content: profile.PanelTLSKeyPEM, Mode: 0o600},
		)
	}
	if profile.PanelAuthToken != "" {
		envContent := "VEIL_API_TOKEN=" + profile.PanelAuthToken + "\n"
		if profile.PanelListen != "" {
			envContent += "VEIL_LISTEN=" + profile.PanelListen + "\n"
		}
		if profile.PanelAccess != "" {
			envContent += "VEIL_PANEL_ACCESS=" + profile.PanelAccess + "\n"
		}
		if profile.Domain != "" {
			envContent += "VEIL_DOMAIN=" + profile.Domain + "\n"
		}
		if profile.Email != "" {
			envContent += "VEIL_EMAIL=" + profile.Email + "\n"
		}
		if profile.PanelTLSEnabled {
			envContent += "VEIL_TLS_CERT=" + panelTLSCertPath + "\n"
			envContent += "VEIL_TLS_KEY=" + panelTLSKeyPath + "\n"
		}
		if profile.WebBasePath != "" && profile.WebBasePath != "/" {
			envContent += "VEIL_WEB_BASE_PATH=" + profile.WebBasePath + "\n"
		}
		files = append(files, managedFile{Path: filepath.Join(paths.EtcDir, "veil.env"), Content: envContent, Mode: 0o600})
	}
	if paths.SystemdDir != "" {
		units := renderer.RenderSystemdUnits(renderer.SystemdConfig{EtcDir: paths.EtcDir, VeilBinary: paths.VeilBinary, CaddyBinary: paths.CaddyBinary})
		unitNames := []string{"veil.service"}
		if profile.InstallNaive || profile.InstallPanelCaddy {
			unitNames = append(unitNames, "veil-naive.service")
		}
		if profile.InstallHysteria2 {
			unitNames = append(unitNames, "veil-hysteria2.service")
		}
		if profile.InstallMieru {
			unitNames = append(unitNames, "veil-mieru.service")
		}
		for _, name := range unitNames {
			files = append(files, managedFile{Path: filepath.Join(paths.SystemdDir, name), Content: units[name], Mode: 0o644})
		}
	}
	return files, nil
}

// isMissingOrBlocked reports whether err means the file cannot be read
// because it doesn't exist or a path component is not a directory (ENOTDIR).
func isMissingOrBlocked(err error) bool {
	return os.IsNotExist(err) || errors.Is(err, syscall.ENOTDIR)
}
