package cli

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/veil-panel/veil/internal/api"
	"github.com/veil-panel/veil/internal/installer"
	"github.com/veil-panel/veil/internal/managementstate"
	"github.com/veil-panel/veil/internal/renderer"
	"github.com/veil-panel/veil/internal/secrets"
)

func buildRepairPlanFromOptions(opts repairWorkflowOptions) (installer.RepairPlan, error) {
	built, err := installer.BuildRURecommendedProfile(installer.RURecommendedInput{
		Secret: randomSecret,
	})
	if err != nil {
		return installer.RepairPlan{}, err
	}
	preserveExistingPanelRepairMaterial(&built, opts.EtcDir)
	veilBinary, executableErr := installExecutableFunc()
	if executableErr != nil {
		veilBinary = ""
	}
	plan, err := installer.BuildRepairPlan(built, installer.ApplyPaths{EtcDir: opts.EtcDir, VarDir: opts.VarDir, SystemdDir: opts.SystemdDir, VeilBinary: veilBinary, CaddyBinary: resolvedRepairBinaryPath("caddy")})
	if err != nil {
		return installer.RepairPlan{}, err
	}
	return addPanelStateRepairActions(plan, opts)
}

func addPanelStateRepairActions(plan installer.RepairPlan, opts repairWorkflowOptions) (installer.RepairPlan, error) {
	statePath := filepath.Join(opts.VarDir, "state.json")
	if _, err := os.Stat(statePath); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return plan, nil
		}
		return installer.RepairPlan{}, err
	}
	store := managementstate.NewStore(statePath, repairStateCipher(filepath.Join(opts.EtcDir, "state.key")))
	snapshot, ok, err := store.Load()
	if err != nil {
		return installer.RepairPlan{}, err
	}
	if !ok {
		return plan, nil
	}
	return newPanelStateRepairMaterial(opts, panelStateRepairSnapshot{
		Settings: snapshot.Settings,
		Inbounds: snapshot.Inbounds,
		Rules:    snapshot.Rules,
		Warp:     snapshot.Warp,
	}).Apply(plan)
}

func applyPanelSettingsRepairActions(plan *installer.RepairPlan, opts repairWorkflowOptions, settings api.Settings) error {
	if settings.PanelAccess == "" {
		return nil
	}
	listen := settings.PanelListen
	if listen == "" {
		listen = "127.0.0.1:2096"
	}
	material := installer.NewPanelManagedMaterial(installer.PanelManagedMaterialInput{
		Paths:           installer.ApplyPaths{EtcDir: opts.EtcDir},
		PanelAuthToken:  repairPanelAuthToken(*plan, opts.EtcDir),
		PanelListen:     listen,
		PanelAccess:     settings.PanelAccess,
		Domain:          settings.Domain,
		Email:           settings.Email,
		WebBasePath:     settings.WebBasePath,
		PanelTLSEnabled: settings.PanelAccess != "caddy",
	})
	if settings.PanelAccess == "caddy" {
		removeRepairActions(plan, material.PanelTLSCertPath(), material.PanelTLSKeyPath())
	}
	return setRepairFileAction(plan, filepath.Join(opts.EtcDir, "veil.env"), material.EnvContent(), 0o600)
}

func repairPanelAuthToken(plan installer.RepairPlan, etcDir string) string {
	token := repairPlanEnvValue(plan, "VEIL_API_TOKEN")
	if token == "" {
		token = readRepairEnv(filepath.Join(etcDir, "veil.env"))["VEIL_API_TOKEN"]
	}
	if token == "" {
		token = randomSecret("panel")
	}
	return token
}

func preserveExistingPanelRepairMaterial(profile *installer.RURecommendedProfile, etcDir string) {
	if profile == nil || etcDir == "" {
		return
	}
	values := readRepairEnv(filepath.Join(etcDir, "veil.env"))
	if token := values["VEIL_API_TOKEN"]; token != "" {
		profile.PanelAuthToken = token
	}
	if listen := values["VEIL_LISTEN"]; listen != "" {
		profile.PanelListen = listen
	}
	if panelAccess := values["VEIL_PANEL_ACCESS"]; panelAccess != "" {
		profile.PanelAccess = panelAccess
	}
	if domain := values["VEIL_DOMAIN"]; domain != "" {
		profile.Domain = domain
	}
	if email := values["VEIL_EMAIL"]; email != "" {
		profile.Email = email
	}
	if webBasePath := values["VEIL_WEB_BASE_PATH"]; webBasePath != "" {
		profile.WebBasePath = webBasePath
	}
	if profile.PanelAccess == "caddy" {
		profile.PanelTLSEnabled = false
		profile.PanelTLSCertPEM = ""
		profile.PanelTLSKeyPEM = ""
		profile.InstallPanelCaddy = true
		caddyfile, err := renderer.RenderPanelCaddyfile(renderer.PanelCaddyConfig{Domain: profile.Domain, Email: profile.Email, PanelPort: panelPortFromListen(profile.PanelListen), WebBasePath: profile.WebBasePath})
		if err == nil {
			profile.Caddyfile = caddyfile
		}
		return
	}
	certPath := values["VEIL_TLS_CERT"]
	if certPath == "" {
		certPath = filepath.Join(etcDir, "panel", "tls.crt")
	}
	keyPath := values["VEIL_TLS_KEY"]
	if keyPath == "" {
		keyPath = filepath.Join(etcDir, "panel", "tls.key")
	}
	cert, certErr := os.ReadFile(certPath)
	key, keyErr := os.ReadFile(keyPath)
	if certErr == nil && keyErr == nil {
		profile.PanelTLSEnabled = true
		profile.PanelTLSCertPEM = string(cert)
		profile.PanelTLSKeyPEM = string(key)
	}
}

func panelPortFromListen(listen string) int {
	_, portString, ok := strings.Cut(strings.TrimSpace(listen), ":")
	if !ok {
		return 0
	}
	for strings.Contains(portString, ":") {
		_, portString, _ = strings.Cut(portString, ":")
	}
	port, err := strconv.Atoi(portString)
	if err != nil {
		return 0
	}
	return port
}

func readRepairEnv(path string) map[string]string {
	body, err := os.ReadFile(path)
	if err != nil {
		return map[string]string{}
	}
	values := map[string]string{}
	for _, line := range strings.Split(string(body), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		values[strings.TrimSpace(key)] = strings.TrimSpace(value)
	}
	return values
}

func resolvedRepairBinaryPath(name string) string {
	path, err := commandLookPath(name)
	if err != nil {
		return ""
	}
	return path
}

func repairStateCipher(keyPath string) *secrets.Cipher {
	if _, err := os.Stat(keyPath); err != nil {
		return nil
	}
	key, err := secrets.LoadOrCreateKey(keyPath)
	if err != nil {
		return nil
	}
	cipher, err := secrets.NewCipher(*key)
	if err != nil {
		return nil
	}
	return cipher
}

func runtimeUnitNamesForState(inbounds []api.Inbound, warp api.WarpConfig) []string {
	return api.NewProtocolRuntimeProvisioning().Plan(inbounds, warp).SystemdUnits()
}

func appendRepairUnit(units []string, unit string) []string {
	for _, existing := range units {
		if existing == unit {
			return units
		}
	}
	return append(units, unit)
}

func repairPlanEnvValue(plan installer.RepairPlan, name string) string {
	prefix := name + "="
	for _, action := range plan.Actions {
		if filepath.Base(action.Path) != "veil.env" {
			continue
		}
		for _, line := range strings.Split(action.Content, "\n") {
			if strings.HasPrefix(line, prefix) {
				return strings.TrimSpace(strings.TrimPrefix(line, prefix))
			}
		}
	}
	return ""
}

func removeRepairActions(plan *installer.RepairPlan, paths ...string) {
	remove := map[string]bool{}
	for _, path := range paths {
		remove[path] = true
	}
	kept := plan.Actions[:0]
	for _, action := range plan.Actions {
		if !remove[action.Path] {
			kept = append(kept, action)
		}
	}
	plan.Actions = kept
}

func setRepairFileAction(plan *installer.RepairPlan, path string, content string, mode os.FileMode) error {
	for idx, action := range plan.Actions {
		if action.Path == path {
			plan.Actions[idx].Content = content
			plan.Actions[idx].Mode = mode
			return nil
		}
	}
	return addRepairFileAction(plan, path, content, mode)
}

func addRepairFileAction(plan *installer.RepairPlan, path string, content string, mode os.FileMode) error {
	for _, action := range plan.Actions {
		if action.Path == path {
			return nil
		}
	}
	body, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			plan.Actions = append(plan.Actions, installer.RepairAction{Path: path, Reason: installer.RepairReasonMissing, Content: content, Mode: mode})
			return nil
		}
		return err
	}
	if string(body) != content {
		plan.Actions = append(plan.Actions, installer.RepairAction{Path: path, Reason: installer.RepairReasonDrifted, Content: content, Mode: mode})
	}
	return nil
}
