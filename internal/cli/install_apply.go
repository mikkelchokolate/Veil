package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/mikkelchokolate/Veil/internal/acmeip"
	"github.com/mikkelchokolate/Veil/internal/caddyassembly"
	"github.com/mikkelchokolate/Veil/internal/caddycapabilities"
	installflow "github.com/mikkelchokolate/Veil/internal/cliflow/install"
	statusflow "github.com/mikkelchokolate/Veil/internal/cliflow/status"
	"github.com/mikkelchokolate/Veil/internal/firewall"
	"github.com/mikkelchokolate/Veil/internal/hostaccess"
	"github.com/mikkelchokolate/Veil/internal/hostenv"
	"github.com/mikkelchokolate/Veil/internal/installer"
	"github.com/mikkelchokolate/Veil/internal/managementstate"
	"github.com/mikkelchokolate/Veil/internal/model"
	"github.com/mikkelchokolate/Veil/internal/renderer"
	"github.com/mikkelchokolate/Veil/internal/secrets"
	"github.com/mikkelchokolate/Veil/internal/service"
	"github.com/mikkelchokolate/Veil/internal/statecommit"
	"github.com/spf13/cobra"
	"golang.org/x/crypto/bcrypt"
)

var installSystemdRunFunc = func(actions []service.SystemdAction) error {
	return service.RunSystemdActions(service.ExecRunner{}, actions)
}

var leIPCertIssueFunc = func(ctx context.Context, opts acmeip.IssueOptions) (acmeip.IssuedCert, error) {
	return acmeip.IssueIPCert(ctx, opts)
}

var installExecutableFunc = os.Executable
var installPrepareHostFunc = hostaccess.Prepare
var installWaitPanelReadyFunc = waitForInstalledPanelReady
var installHelperSocketPath = "/run/veil/helper.sock"
var installPanelReadyTimeout = 15 * time.Second
var installFirewallApplyFunc = func(rules []firewall.Rule) error {
	// Firewall management requires root. In tests and staging installs that run
	// as an unprivileged user we silently skip applying rules rather than fail.
	if os.Geteuid() != 0 {
		return nil
	}
	return firewall.NewUFWApplier().ApplySafely(rules)
}

func applyRURecommendedInstall(cmd *cobra.Command, profile installer.RURecommendedProfile, opts ruRecommendedInstallOptions) error {
	actualBackupDir := opts.BackupDir
	if !opts.BackupDirSet {
		actualBackupDir = filepath.Join(opts.VarDir, "backups")
	}
	systemdDir := opts.SystemdDir
	if systemdDir == "" {
		systemdDir = defaultSystemdDir
	}
	veilBinary, err := installExecutableFunc()
	if err != nil {
		veilBinary = ""
	}

	// 1. Ensure configuration and state directories exist before running install and writing files
	if err := os.MkdirAll(opts.EtcDir, 0755); err != nil {
		return fmt.Errorf("create etc directory: %w", err)
	}
	if err := os.MkdirAll(filepath.Join(opts.EtcDir, "certs"), 0700); err != nil {
		return fmt.Errorf("create certs directory: %w", err)
	}
	if err := os.MkdirAll(opts.VarDir, 0755); err != nil {
		return fmt.Errorf("create var directory: %w", err)
	}

	// 1a. In direct mode, the panel endpoint is the public IP. Resolve it once
	// and use it both for the client-link domain and for the LE IP certificate.
	resolvedIP, _ := resolvePublicIPForDirectInstall(cmd.Context(), opts)
	if resolvedIP != nil && profile.PanelAccess == "direct" && profile.Domain == "" {
		profile.Domain = resolvedIP.String()
	}

	// 2. Initialize state.key and encrypted state.json with generated credentials
	resolvedKeyPath := filepath.Join(opts.EtcDir, "state.key")
	resolvedStatePath := filepath.Join(opts.VarDir, "state.json")

	key, err := secrets.LoadOrCreateKey(resolvedKeyPath)
	if err != nil {
		return fmt.Errorf("initialize encryption key: %w", err)
	}
	cipher, err := secrets.NewCipher(*key)
	if err != nil {
		return fmt.Errorf("create cipher: %w", err)
	}

	stateExists := false
	if _, err := os.Stat(resolvedStatePath); err == nil {
		stateExists = true
	}

	reusedExistingState := false
	var snapshot model.ManagementSnapshot
	if stateExists {
		// Reuse the existing state.json so an in-place reinstall keeps the admin
		// login and the secret web base path.
		store := managementstate.NewStore(resolvedStatePath, cipher)
		loaded, ok, err := store.Load()
		snapshot = loaded
		if err != nil || !ok {
			// State is present but unreadable (corrupted, or the key no longer
			// matches). Stop instead of silently leaving a panel nobody can log
			// into or overwriting potentially recoverable data.
			return fmt.Errorf(
				"found existing Veil state at %s but could not read it with the encryption key at %s (corrupted state or a mismatched key).\n"+
					"To start fresh: run `sudo veil uninstall` to remove the old state, then reinstall.\n"+
					"To keep the existing data: restore the matching %s before reinstalling",
				resolvedStatePath, resolvedKeyPath, filepath.Base(resolvedKeyPath))
		}
		if snapshot.Settings.WebBasePath != "" {
			// The panel Caddyfile was already rendered with a freshly-generated
			// base path before we knew we'd reuse the existing one. Rewrite it to
			// the reused base path so Caddy routes the same path the panel actually
			// serves (VEIL_WEB_BASE_PATH); otherwise an in-place switch to caddy
			// mode 404s.
			if profile.CaddyJSON != "" && profile.WebBasePath != "" {
				profile.CaddyJSON = strings.ReplaceAll(profile.CaddyJSON, profile.WebBasePath, snapshot.Settings.WebBasePath)
			}
			profile.WebBasePath = snapshot.Settings.WebBasePath
		}
		// Use the first admin user's username
		for _, u := range snapshot.Users {
			if u.Role == "admin" {
				profile.Username = u.Username
				break
			}
		}
		profile.Password = "" // clear it, as we didn't generate a new one
		reusedExistingState = true
		// Direct mode needs a domain for client links. If the existing state
		// was created before auto-fill was implemented, backfill it now.
		if resolvedIP != nil && snapshot.Settings.Domain == "" {
			if _, err := statecommit.Update(statecommit.UpdateOptions{
				StatePath: resolvedStatePath, KeyPath: resolvedKeyPath,
			}, func(current *model.ManagementSnapshot) error {
				if current.Settings.Domain == "" {
					current.Settings.Domain = resolvedIP.String()
				}
				snapshot = *current
				return nil
			}); err != nil {
				return fmt.Errorf("update existing panel state domain: %w", err)
			}
		}
	} else {
		hashed, err := bcrypt.GenerateFromPassword([]byte(profile.Password), 10)
		if err != nil {
			return fmt.Errorf("hash admin password: %w", err)
		}

		defaultState := managementstate.BuildDefaultState(managementstate.DefaultInput{
			PanelListen: profile.PanelListen,
			PanelAccess: profile.PanelAccess,
			WebBasePath: profile.WebBasePath,
			Domain:      profile.Domain,
			Email:       profile.Email,
		})

		initialSnapshot := model.ManagementSnapshot{
			Settings: defaultState.Settings,
			Users: []model.User{
				{
					Username:     profile.Username,
					PasswordHash: string(hashed),
					Role:         "admin",
				},
			},
		}
		managementstate.CompleteSetupForAdmins(&initialSnapshot, time.Now())

		if _, err := statecommit.Save(initialSnapshot, statecommit.Options{StatePath: resolvedStatePath, Cipher: cipher}); err != nil {
			return fmt.Errorf("write initial state.json: %w", err)
		}
	}

	if reusedExistingState && profile.InstallPanelCaddy {
		profile.CaddyJSON = retainCaddyJSONOnReinstall(profile, snapshot, opts.EtcDir)
	}

	// 2a. Open the planned ACME HTTP-01 port (and the rest of the install
	// firewall plan) before requesting the certificate. Fall back to the
	// self-signed certificate already in the profile if issuance fails.
	installPlan, planErr := buildInstallPlan(profile, opts)
	if planErr == nil && len(installPlan.FirewallActions) > 0 {
		if err := installFirewallApplyFunc(installPlan.FirewallActions); err != nil {
			_ = writeAuditInstall(opts.AuditLog, "", false, err.Error(), nil)
			return fmt.Errorf("apply firewall rules: %w", err)
		}
	}
	if opts.PanelAccess == "direct" && opts.LEIPCert {
		if err := issueLEIPCertForProfile(cmd.Context(), &profile, opts, resolvedIP); err != nil {
			fmt.Fprintf(cmd.ErrOrStderr(), "WARNING: could not obtain Let's Encrypt IP certificate: %v\n", err)
			fmt.Fprintln(cmd.ErrOrStderr(), "Falling back to the generated self-signed certificate.")
		}
	}

	// 3. Create the veil service account before writing/chowning secrets.
	// installer.Apply chowns generated files to the veil user; that lookup
	// fails on a fresh host if useradd has not run yet.
	if shouldPrepareInstallHost(systemdDir) {
		if err := installPrepareHostFunc(hostaccess.Paths{EtcDir: opts.EtcDir, VarDir: opts.VarDir}); err != nil {
			_ = writeAuditInstall(opts.AuditLog, "", false, err.Error(), nil)
			return fmt.Errorf("prepare panel service account and permissions: %w", err)
		}
	}

	// 4. Apply profile configurations (veil.env, caddyfile, systemd unit files, etc.)
	result, err := installApplyFunc(profile, installer.ApplyPaths{
		EtcDir:      opts.EtcDir,
		VarDir:      opts.VarDir,
		SystemdDir:  systemdDir,
		BackupDir:   actualBackupDir,
		VeilBinary:  veilBinary,
		CaddyBinary: opts.CaddyBinary,
	})
	if err != nil {
		_ = writeAuditInstall(opts.AuditLog, result.BackupID, false, err.Error(), nil)
		return err
	}

	if shouldPrepareInstallHost(systemdDir) {
		if err := installSystemdRunFunc(service.SystemdApplyPlan(installer.PanelSystemdUnits(profile))); err != nil {
			_ = writeAuditInstall(opts.AuditLog, result.BackupID, false, err.Error(), result.WrittenFiles)
			return err
		}
		if err := installWaitPanelReadyFunc(cmd, profile, opts); err != nil {
			_ = writeAuditInstall(opts.AuditLog, result.BackupID, false, err.Error(), result.WrittenFiles)
			return fmt.Errorf("panel did not become ready: %w", err)
		}
	}
	fmt.Fprintln(cmd.OutOrStdout(), "Written files:")
	for _, path := range result.WrittenFiles {
		fmt.Fprintf(cmd.OutOrStdout(), "- %s\n", path)
	}
	fmt.Fprintf(cmd.OutOrStdout(), "- %s\n", resolvedStatePath)
	fmt.Fprintln(cmd.OutOrStdout())
	fmt.Fprint(cmd.OutOrStdout(), installflow.CredentialSummary(profile))
	if reusedExistingState {
		fmt.Fprintf(cmd.OutOrStdout(), "Reused the existing admin login and panel path from %s; your previous password still applies.\n", resolvedStatePath)
		fmt.Fprintf(cmd.OutOrStdout(), "To set a new password: sudo veil admin set --username %s --password 'NEW' --role admin\n", profile.Username)
		fmt.Fprintln(cmd.OutOrStdout(), "To wipe and start fresh instead: run sudo veil uninstall, then reinstall.")
	}
	if err := writeAuditInstall(opts.AuditLog, result.BackupID, true, "", result.WrittenFiles); err != nil {
		return fmt.Errorf("audit log write failed after successful install: %w", err)
	}
	return nil
}

func shouldPrepareInstallHost(systemdDir string) bool {
	return filepath.Clean(systemdDir) == filepath.Clean(defaultSystemdDir)
}

func buildInstallPlan(profile installer.RURecommendedProfile, opts ruRecommendedInstallOptions) (installer.InstallPlan, error) {
	platform := hostenv.CurrentPlatform()
	if platform.OS != "linux" {
		platform.OS = "linux"
	}
	caddyBinary := opts.CaddyBinary
	if profile.InstallPanelCaddy && caddyBinary == "" {
		if path, err := execLookPath("caddy"); err == nil {
			caddyBinary = path
		}
	}
	// The actual panel port is embedded in the rendered profile (e.g.
	// "0.0.0.0:25500"); opts.PanelPort is 0 when a random port was chosen.
	panelPort := opts.PanelPort
	if _, port, err := net.SplitHostPort(profile.PanelListen); err == nil {
		if parsed, parseErr := parsePort(port); parseErr == nil {
			panelPort = parsed
		}
	}
	if profile.InstallPanelCaddy {
		panelPort = 0
	}
	leIPCertPort := 0
	if profile.PanelAccess == "direct" && opts.LEIPCert {
		leIPCertPort = opts.LEIPCertPort
	}
	return installer.BuildInstallPlan(profile, installer.InstallPlanInput{
		Platform:     platform,
		SystemdUnits: installer.PanelSystemdUnits(profile),
		PanelAccess:  profile.PanelAccess,
		PanelPort:    panelPort,
		CaddyBinary:  caddyBinary,
		LEIPCertPort: leIPCertPort,
	})
}

func issueLEIPCertForProfile(ctx context.Context, profile *installer.RURecommendedProfile, opts ruRecommendedInstallOptions, resolvedIP net.IP) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if resolvedIP == nil {
		publicIP := opts.PublicIP
		if publicIP == "" {
			publicIP = "auto"
		}
		var err error
		resolvedIP, err = hostenv.ResolvePublicIP(ctx, publicIP, installPublicIPClient, installPublicIPEndpoints)
		if err != nil {
			return fmt.Errorf("detect public IP: %w", err)
		}
		if resolvedIP == nil {
			return fmt.Errorf("public IP detection returned empty")
		}
	}

	certPath := filepath.Join(opts.EtcDir, "panel", "tls.crt")
	keyPath := filepath.Join(opts.EtcDir, "panel", "tls.key")
	if err := os.MkdirAll(filepath.Dir(certPath), 0o750); err != nil {
		return fmt.Errorf("create panel cert directory: %w", err)
	}
	cert, err := leIPCertIssueFunc(ctx, acmeip.IssueOptions{
		PublicIPv4: resolvedIP.String(),
		HTTPPort:   opts.LEIPCertPort,
		Email:      opts.Email,
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

func parsePort(s string) (int, error) {
	var port int
	_, err := fmt.Sscanf(s, "%d", &port)
	return port, err
}

func resolvePublicIPForDirectInstall(ctx context.Context, opts ruRecommendedInstallOptions) (net.IP, error) {
	if opts.PanelAccess != "direct" {
		return nil, nil
	}
	publicIP := opts.PublicIP
	if publicIP == "" {
		publicIP = "auto"
	}
	return hostenv.ResolvePublicIP(ctx, publicIP, installPublicIPClient, installPublicIPEndpoints)
}

var execLookPath = exec.LookPath

func waitForInstalledPanelReady(cmd *cobra.Command, profile installer.RURecommendedProfile, opts ruRecommendedInstallOptions) error {
	ctx := context.Background()
	if cmd != nil && cmd.Context() != nil {
		ctx = cmd.Context()
	}
	ctx, cancel := context.WithTimeout(ctx, installPanelReadyTimeout)
	defer cancel()

	certPath := ""
	serverName := ""
	if profile.PanelTLSEnabled {
		certPath = filepath.Join(opts.EtcDir, "panel", "tls.crt")
		serverName = profile.Domain
		if serverName == "" {
			serverName = "localhost"
		}
	}
	contract := statusflow.ContractFromServe(profile.PanelListen, profile.PanelTLSEnabled, profile.WebBasePath, certPath, serverName)
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	var last error
	for {
		last = probeInstalledPanel(ctx, contract, profile.PanelAuthToken)
		if last == nil {
			if err := helperSocketReady(); err != nil {
				last = err
			} else {
				return nil
			}
		}
		select {
		case <-ctx.Done():
			if last == nil {
				last = ctx.Err()
			}
			return fmt.Errorf("%w; check journalctl -u veil.service veil-helper.socket", last)
		case <-ticker.C:
		}
	}
}

func helperSocketReady() error {
	if _, err := os.Lstat(installHelperSocketPath); err != nil {
		return fmt.Errorf("helper socket %s: %w", installHelperSocketPath, err)
	}
	return nil
}

func probeInstalledPanel(ctx context.Context, contract statusflow.ContainerHealthContract, token string) error {
	url, err := statusflow.LocalHealthURL(contract)
	if err != nil {
		return err
	}
	client, err := statusflow.HealthHTTPClient(contract)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	if token != "" {
		req.Header.Set("X-Veil-Token", token)
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("panel healthz %s: %w", url, err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("panel healthz %s: %s", url, resp.Status)
	}
	if !strings.Contains(strings.ToLower(resp.Header.Get("Content-Type")), "application/json") {
		return fmt.Errorf("panel healthz %s is not a Veil instance (content-type %q)", url, resp.Header.Get("Content-Type"))
	}
	var payload struct {
		Status string `json:"status"`
	}
	if err := json.Unmarshal(body, &payload); err != nil || payload.Status != "ok" {
		return fmt.Errorf("panel healthz %s is not a Veil instance", url)
	}
	return nil
}

var installProbeCaddyCapabilities = caddycapabilities.Probe

func retainCaddyJSONOnReinstall(profile installer.RURecommendedProfile, snapshot model.ManagementSnapshot, etcDir string) string {
	livePath := filepath.Join(etcDir, "generated", "caddy", "config.json")
	live := ""
	if body, err := os.ReadFile(livePath); err == nil {
		live = strings.TrimSpace(string(body))
	}
	if rendered, err := renderCaddyJSONFromSnapshot(snapshot, profile); err == nil && strings.TrimSpace(rendered) != "" {
		if len(snapshot.Inbounds) > 0 || live == "" {
			return rendered
		}
	}
	if live != "" {
		return live
	}
	return profile.CaddyJSON
}

func renderCaddyJSONFromSnapshot(snapshot model.ManagementSnapshot, profile installer.RURecommendedProfile) (string, error) {
	settings := snapshot.Settings
	if settings.PanelAccess == "" {
		settings.PanelAccess = "caddy"
	}
	if settings.WebBasePath == "" {
		settings.WebBasePath = profile.WebBasePath
	}
	if settings.Domain == "" {
		settings.Domain = profile.Domain
	}
	if settings.Email == "" {
		settings.Email = profile.Email
	}
	if settings.PanelDomain == "" {
		settings.PanelDomain = settings.Domain
	}
	if settings.PanelEmail == "" {
		settings.PanelEmail = settings.Email
	}
	if settings.PanelListen == "" {
		settings.PanelListen = profile.PanelListen
	}
	plan, _, _, err := caddyassembly.BuildFinalRenderPlan(settings, snapshot.Inbounds)
	if err != nil {
		return "", err
	}
	caps, err := installProbeCaddyCapabilities("")
	if err != nil {
		if !caddycapabilities.IsMissingBinary(err) {
			return "", err
		}
		caps = caddycapabilities.CaddyCapabilities{}
	}
	if snapshotHasNaiveInbound(snapshot) {
		caps.ForwardProxy = true
	}
	body, err := renderer.RenderCaddyJSON(plan, caps)
	if err != nil {
		return "", err
	}
	return string(body), nil
}

func snapshotHasNaiveInbound(snapshot model.ManagementSnapshot) bool {
	for _, inbound := range snapshot.Inbounds {
		if inbound.Enabled && inbound.Protocol == "naiveproxy" {
			return true
		}
	}
	return false
}
