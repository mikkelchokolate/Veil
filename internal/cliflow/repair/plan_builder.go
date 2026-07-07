package repair

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/mikkelchokolate/Veil/internal/acmeip"
	"github.com/mikkelchokolate/Veil/internal/api"
	"github.com/mikkelchokolate/Veil/internal/hostenv"
	"github.com/mikkelchokolate/Veil/internal/installer"
	"github.com/mikkelchokolate/Veil/internal/managementstate"
	"github.com/mikkelchokolate/Veil/internal/panelmaterial"
	"github.com/mikkelchokolate/Veil/internal/renderer"
	"github.com/mikkelchokolate/Veil/internal/runtime"
	"github.com/mikkelchokolate/Veil/internal/secrets"
	"github.com/mikkelchokolate/Veil/internal/service"
)

type PlanDependencies struct {
	Secret     installer.SecretFunc
	Executable func() (string, error)
	LookPath   func(string) (string, error)
}

var leIPCertIssueFunc = acmeip.IssueIPCert
var repairPublicIPResolver = hostenv.ResolvePublicIP
var repairLEPublicIPResolver = hostenv.ResolvePublicIP

func BuildPlanFromOptions(opts Options, deps PlanDependencies) (installer.RepairPlan, error) {
	secret := deps.secret()
	// Read existing panel configuration so the regenerated TLS certificate
	// matches the actual access mode (direct mode needs interface IPs in SANs).
	existing := readRepairEnv(filepath.Join(opts.EtcDir, "veil.env"))
	panelAccess := existing["VEIL_PANEL_ACCESS"]
	if panelAccess == "" {
		panelAccess = "local"
	}
	panelPort := panelPortFromListen(existing["VEIL_LISTEN"])
	built, err := installer.BuildRURecommendedProfile(installer.RURecommendedInput{
		Secret:      secret,
		PanelAccess: panelAccess,
		PanelPort:   panelPort,
		Domain:      existing["VEIL_DOMAIN"],
		Email:       existing["VEIL_EMAIL"],
	})
	if err != nil {
		return installer.RepairPlan{}, err
	}
	preserveExistingPanelRepairMaterial(&built, opts.EtcDir)

	// Direct mode uses the public IP as the panel endpoint. Resolve it once so
	// both veil.env and the encrypted panel state get a usable Domain.
	resolvedIP := ""
	if built.PanelAccess == "direct" && built.Domain == "" {
		ip, err := repairPublicIPResolver(context.Background(), opts.PublicIP, nil, nil)
		if err == nil && ip != nil {
			resolvedIP = ip.String()
			built.Domain = resolvedIP
		}
	}

	if opts.LEIPCert && built.PanelAccess == "direct" {
		if err := maybeIssueLEIPCert(context.Background(), &built, opts); err != nil {
			// Repair should not fail because a certificate could not be renewed;
			// the existing self-signed cert from the profile is still usable.
			// The error is intentionally swallowed here; callers may log it later.
			_ = err
		}
	}
	veilBinary, executableErr := deps.executable()()
	if executableErr != nil {
		veilBinary = ""
	}
	plan, err := installer.BuildRepairPlan(built, installer.ApplyPaths{EtcDir: opts.EtcDir, VarDir: opts.VarDir, SystemdDir: opts.SystemdDir, VeilBinary: veilBinary, CaddyBinary: deps.resolvedBinaryPath("caddy")})
	if err != nil {
		return installer.RepairPlan{}, err
	}
	return addPanelStateRepairActions(plan, opts, deps, resolvedIP)
}

func (d PlanDependencies) secret() installer.SecretFunc {
	if d.Secret != nil {
		return d.Secret
	}
	return func(label string) string { return label }
}

func (d PlanDependencies) executable() func() (string, error) {
	if d.Executable != nil {
		return d.Executable
	}
	return os.Executable
}

func (d PlanDependencies) lookPath() func(string) (string, error) {
	if d.LookPath != nil {
		return d.LookPath
	}
	return exec.LookPath
}

func addPanelStateRepairActions(plan installer.RepairPlan, opts Options, deps PlanDependencies, resolvedIP string) (installer.RepairPlan, error) {
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
	// Direct mode needs a domain for client links. Backfill it in the encrypted
	// state when it is empty and we were able to resolve the public IP.
	if resolvedIP != "" && snapshot.Settings.Domain == "" && !opts.DryRun {
		snapshot.Settings.Domain = resolvedIP
		if err := store.Save(snapshot); err != nil {
			return installer.RepairPlan{}, fmt.Errorf("update panel state domain: %w", err)
		}
	}
	return newPanelStateRepairMaterial(opts, panelStateRepairSnapshot{
		Settings: snapshot.Settings,
		Inbounds: snapshot.Inbounds,
		Rules:    snapshot.Rules,
		Warp:     snapshot.Warp,
	}, deps).Apply(plan)
}

func applyPanelSettingsRepairActions(plan *installer.RepairPlan, opts Options, settings api.Settings, secret installer.SecretFunc) error {
	if settings.PanelAccess == "" {
		return nil
	}
	listen := settings.PanelListen
	if listen == "" {
		listen = "127.0.0.1:2096"
	}
	material := panelmaterial.NewManagedMaterial(panelmaterial.Input{
		Paths:           panelmaterial.Paths{EtcDir: opts.EtcDir},
		PanelAuthToken:  repairPanelAuthToken(*plan, opts.EtcDir, secret),
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

func repairPanelAuthToken(plan installer.RepairPlan, etcDir string, secret installer.SecretFunc) string {
	token := repairPlanEnvValue(plan, "VEIL_API_TOKEN")
	if token == "" {
		token = readRepairEnv(filepath.Join(etcDir, "veil.env"))["VEIL_API_TOKEN"]
	}
	if token == "" {
		token = secret("panel")
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

func (d PlanDependencies) resolvedBinaryPath(name string) string {
	path, err := d.lookPath()(name)
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
	return service.NewProtocolRuntimeProvisioning(api.NewManagedRuntimeCatalogFor(inbounds, warp)).Plan(inbounds, warp).SystemdUnits()
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

func maybeIssueLEIPCert(ctx context.Context, profile *installer.RURecommendedProfile, opts Options) error {
	certPath := filepath.Join(opts.EtcDir, "panel", "tls.crt")
	if !shouldRenewLEIPCert(certPath) {
		return nil
	}

	publicIP := opts.PublicIP
	if publicIP == "" {
		publicIP = "auto"
	}
	resolvedIP, err := repairLEPublicIPResolver(ctx, publicIP, nil, nil)
	if err != nil {
		return fmt.Errorf("detect public IP: %w", err)
	}
	if resolvedIP == nil {
		return fmt.Errorf("public IP detection returned empty")
	}

	keyPath := filepath.Join(opts.EtcDir, "panel", "tls.key")
	cert, err := leIPCertIssueFunc(ctx, acmeip.IssueOptions{
		PublicIPv4: resolvedIP.String(),
		HTTPPort:   opts.LEIPCertPort,
		Email:      profile.Email,
		CertPath:   certPath,
		KeyPath:    keyPath,
	})
	if err != nil {
		return err
	}

	certPEM, err := os.ReadFile(cert.CertPath)
	if err != nil {
		return fmt.Errorf("read issued certificate: %w", err)
	}
	keyPEM, err := os.ReadFile(cert.KeyPath)
	if err != nil {
		return fmt.Errorf("read issued key: %w", err)
	}
	profile.PanelTLSCertPEM = string(certPEM)
	profile.PanelTLSKeyPEM = string(keyPEM)
	return nil
}

func shouldRenewLEIPCert(certPath string) bool {
	info := runtime.ReadTLSCert(certPath)
	if !info.Valid || info.Error != "" {
		return true
	}
	if info.DaysRemaining <= 7 {
		return true
	}
	if !strings.Contains(info.Issuer, "Let's Encrypt") {
		return true
	}
	return false
}
