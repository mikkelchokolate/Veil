package cli

import (
	"context"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/mikkelchokolate/Veil/internal/acmeip"
	"github.com/mikkelchokolate/Veil/internal/apply"
	"github.com/mikkelchokolate/Veil/internal/caddyassembly"
	"github.com/mikkelchokolate/Veil/internal/hostenv"
	"github.com/mikkelchokolate/Veil/internal/managementstate"
	"github.com/mikkelchokolate/Veil/internal/model"
	"github.com/mikkelchokolate/Veil/internal/privileged"
	"github.com/mikkelchokolate/Veil/internal/runtime"
	"github.com/mikkelchokolate/Veil/internal/secrets"
	"github.com/mikkelchokolate/Veil/internal/storage"
	"github.com/spf13/cobra"
)

// newCertCommand registers `veil cert status` and `veil cert renew` — the
// operator surface of the short-lived panel IP certificate whose lifecycle
// the daemon owns (#1169/#1170).
func newCertCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "cert",
		Short: "Inspect and renew the panel Let's Encrypt IP certificate",
	}
	// CLI invocations run outside veil.service's EnvironmentFile, so the
	// persisted lifecycle knobs in veil.env are resolved from <etc-dir>
	// explicitly (#1189); the flags only override where that file lives.
	var certEtcDir string
	var certVarDir string
	cmd.PersistentFlags().StringVar(&certEtcDir, "etc-dir", "", "Veil configuration directory (defaults to VEIL_ETC_DIR or /etc/veil)")
	cmd.PersistentFlags().StringVar(&certVarDir, "var-dir", "", "Veil state directory (defaults to VEIL_VAR_DIR or /var/lib/veil)")

	var statusJSON bool
	statusCmd := &cobra.Command{
		Use:   "status",
		Short: "Show the panel IP certificate state",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runCertStatus(cmd, statusJSON, certEtcDir)
		},
	}
	statusCmd.Flags().BoolVar(&statusJSON, "json", false, "print the certificate status as JSON")
	cmd.AddCommand(statusCmd)

	var renewPublicIP string
	var renewEmail string
	var renewPort int
	renewCmd := &cobra.Command{
		Use:   "renew",
		Short: "Force renewal of the panel IP certificate via the privileged helper",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runCertRenew(cmd, renewPublicIP, renewEmail, renewPort, certEtcDir, certVarDir)
		},
	}
	renewCmd.Flags().StringVar(&renewPublicIP, "public-ip", "auto", "public IPv4/IPv6 the certificate covers; auto detects both families; unset uses the install-time persisted value")
	renewCmd.Flags().StringVar(&renewEmail, "email", "", "ACME account contact email (defaults to the panel settings email)")
	renewCmd.Flags().IntVar(&renewPort, "port", 80, "port used for Let's Encrypt HTTP-01 validation (defaults to the install-time persisted port)")
	cmd.AddCommand(renewCmd)
	return cmd
}

// certResolveDirs applies the --etc-dir/--var-dir flag defaults over the
// hostenv resolution chain.
func certResolveDirs(etcDir, varDir string) (string, string) {
	if strings.TrimSpace(etcDir) != "" {
		etcDir = filepath.Clean(etcDir)
	} else {
		etcDir = hostenv.EtcDir()
	}
	if strings.TrimSpace(varDir) != "" {
		varDir = filepath.Clean(varDir)
	} else {
		varDir = hostenv.VarDir()
	}
	return etcDir, varDir
}

// certEnvFile loads the persisted veil.env map for the resolved etc dir so
// the CLI honors the same lifecycle knobs the daemon sees through
// EnvironmentFile (#1189).
var certEnvFile = hostenv.ReadEnvFile

// certTLSCertPath resolves the panel TLS certificate path the same way the
// status flow does: VEIL_TLS_CERT first (process env, then the persisted
// veil.env value), then <etc>/panel/tls.crt (#1189).
func certTLSCertPath(envValues map[string]string, etcDir string) string {
	if v := hostenv.EnvOrFile(envValues, "VEIL_TLS_CERT"); v != "" {
		return v
	}
	return filepath.Join(etcDir, "panel", "tls.crt")
}

func certTLSKeyPath(envValues map[string]string, certPath, etcDir string) string {
	if v := hostenv.EnvOrFile(envValues, "VEIL_TLS_KEY"); v != "" {
		return v
	}
	if hostenv.EnvOrFile(envValues, "VEIL_TLS_CERT") != "" {
		return strings.TrimSuffix(certPath, filepath.Ext(certPath)) + ".key"
	}
	return filepath.Join(etcDir, "panel", "tls.key")
}

// certFlagChanged reports whether a flag was explicitly set, nil-safe for
// direct test invocations on bare commands.
func certFlagChanged(cmd *cobra.Command, name string) bool {
	f := cmd.Flags().Lookup(name)
	return f != nil && f.Changed
}

type certStatusView struct {
	runtime.TLSCertInfo
	KeyPath        string   `json:"keyPath"`
	IPAddresses    []string `json:"ipAddresses,omitempty"`
	NeedsRenewal   bool     `json:"needsRenewal"`
	RenewalWindowH int      `json:"renewalWindowHours"`
}

// certIPSANs extracts the IP SANs ReadTLSCert does not surface — the whole
// point of this certificate is its IP identity, so status must show it.
func certIPSANs(certPath string) []string {
	data, err := os.ReadFile(certPath)
	if err != nil {
		return nil
	}
	block, _ := pem.Decode(data)
	if block == nil {
		return nil
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil
	}
	out := make([]string, 0, len(cert.IPAddresses))
	for _, ip := range cert.IPAddresses {
		out = append(out, ip.String())
	}
	return out
}

func runCertStatus(cmd *cobra.Command, jsonOutput bool, etcDirFlag string) error {
	etcDir, _ := certResolveDirs(etcDirFlag, "")
	envValues := certEnvFile(filepath.Join(etcDir, "veil.env"))
	certPath := certTLSCertPath(envValues, etcDir)
	info := runtime.ReadTLSCert(certPath)
	view := certStatusView{
		TLSCertInfo:    info,
		KeyPath:        certTLSKeyPath(envValues, certPath, etcDir),
		IPAddresses:    certIPSANs(certPath),
		NeedsRenewal:   acmeip.NeedsRenewal(certPath, time.Now()),
		RenewalWindowH: int(acmeip.RenewalWindow / time.Hour),
	}
	if jsonOutput {
		encoded, err := json.MarshalIndent(view, "", "  ")
		if err != nil {
			return err
		}
		fmt.Fprintln(cmd.OutOrStdout(), string(encoded))
		return nil
	}
	out := cmd.OutOrStdout()
	fmt.Fprintf(out, "Certificate: %s\n", info.Path)
	fmt.Fprintf(out, "Key:         %s\n", view.KeyPath)
	if info.Error != "" {
		fmt.Fprintf(out, "State:       error: %s\n", info.Error)
	} else if info.Valid {
		fmt.Fprintf(out, "State:       valid\n")
	} else {
		fmt.Fprintf(out, "State:       invalid or expired\n")
	}
	if info.Issuer != "" {
		fmt.Fprintf(out, "Issuer:      %s\n", info.Issuer)
	}
	if len(info.DNSNames) > 0 || len(view.IPAddresses) > 0 {
		names := append(append([]string(nil), info.DNSNames...), view.IPAddresses...)
		fmt.Fprintf(out, "Identities:  %s\n", strings.Join(names, ", "))
	}
	if info.NotAfter != "" {
		fmt.Fprintf(out, "Expires:     %s (%d days remaining)\n", info.NotAfter, info.DaysRemaining)
	}
	if view.NeedsRenewal {
		fmt.Fprintf(out, "Renewal:     required (window: %dh before expiry)\n", view.RenewalWindowH)
	} else {
		fmt.Fprintf(out, "Renewal:     not required (window: %dh before expiry)\n", view.RenewalWindowH)
	}
	return nil
}

// certRenewIssuer is the privileged-op seam so tests can stub the socket.
var certRenewIssuer = func(socketPath string) privileged.IPCertIssuer {
	return privileged.NewSocketClient(socketPath)
}

// mintCertRenewFence claims the durable apply lease so the helper — running
// with RequireFence — accepts the renewal. The lease lives in veil.db next
// to the state file, shared with the running panel: while an apply holds it
// the CLI renewal reports the conflict instead of racing the apply's
// generation, and vice versa.
func mintCertRenewFence(varDir string) (privileged.FenceToken, func(), error) {
	databasePath := filepath.Join(varDir, "veil.db")
	db, err := storage.Open(databasePath)
	if err != nil {
		return privileged.FenceToken{}, nil, fmt.Errorf("open fencing lease store %s: %w", databasePath, err)
	}
	store := apply.NewLeaseStore(db)
	now := time.Now().UTC()
	owner := fmt.Sprintf("pid:%d:%s", os.Getpid(), uuid.NewString())
	lease, acquired, err := store.Acquire(owner, "cert-renew", now, 30*time.Minute)
	if err != nil {
		_ = db.Close()
		return privileged.FenceToken{}, nil, fmt.Errorf("acquire fencing lease: %w", err)
	}
	if !acquired {
		_ = db.Close()
		return privileged.FenceToken{}, nil, errors.New("another operation holds the runtime fencing lease; retry after it finishes")
	}
	release := func() {
		_ = store.Release(lease.Owner, lease.Generation)
		_ = db.Close()
	}
	return privileged.FenceToken{
		Owner:          lease.Owner,
		Generation:     lease.Generation,
		LeaseExpiresAt: lease.ExpiresAt,
		OperationID:    lease.Operation,
	}, release, nil
}

// certRenewFence is a test seam around mintCertRenewFence.
var certRenewFence = mintCertRenewFence

// loadCertSnapshot loads the persisted panel state so `cert renew` can reuse
// install-time facts the bare shell environment does not carry: whether the
// rendered plan keeps a Caddy listener on :80 (which decides the
// Caddy-fronted acme.sh standalone port, #1181) and the panel email the
// --email help promises as the default (#1189). Best-effort — a missing or
// unreadable snapshot just leaves the flag defaults.
func loadCertSnapshot(etcDir, varDir string) (model.ManagementSnapshot, bool) {
	statePath := filepath.Join(varDir, "state.json")
	if v := strings.TrimSpace(os.Getenv("VEIL_STATE_PATH")); v != "" {
		statePath = v
	}
	keyPath := filepath.Join(etcDir, "state.key")
	if v := strings.TrimSpace(os.Getenv("VEIL_KEY_PATH")); v != "" {
		keyPath = v
	}
	if _, err := os.Stat(statePath); err != nil {
		return model.ManagementSnapshot{}, false
	}
	if _, err := os.Stat(keyPath); err != nil {
		return model.ManagementSnapshot{}, false
	}
	key, err := secrets.LoadOrCreateKey(keyPath)
	if err != nil {
		return model.ManagementSnapshot{}, false
	}
	cipher, err := secrets.NewCipher(*key)
	if err != nil {
		return model.ManagementSnapshot{}, false
	}
	snapshot, ok, err := managementstate.NewStore(statePath, cipher).Load()
	if err != nil || !ok {
		return model.ManagementSnapshot{}, false
	}
	return snapshot, true
}

func runCertRenew(cmd *cobra.Command, publicIP, email string, port int, etcDirFlag, varDirFlag string) error {
	ctx := cmd.Context()
	if ctx == nil {
		ctx = context.Background()
	}
	etcDir, varDir := certResolveDirs(etcDirFlag, varDirFlag)
	envValues := certEnvFile(filepath.Join(etcDir, "veil.env"))

	// The persisted --le-ip-cert=false opt-out applies here too: a manual
	// renewal would issue a certificate the daemon then refuses to renew,
	// silently lapsing the panel onto an expired cert. Renewing anyway is
	// still possible via an explicit VEIL_PANEL_LE_IP_CERT=1 env override
	// (#1187).
	if v := hostenv.EnvOrFile(envValues, "VEIL_PANEL_LE_IP_CERT"); v != "" {
		if enabled, err := strconv.ParseBool(v); err == nil && !enabled {
			fmt.Fprintln(cmd.OutOrStdout(), "Panel IP certificate is disabled (VEIL_PANEL_LE_IP_CERT=0); nothing to renew.")
			return nil
		}
	}

	// Public-IP precedence: an explicit --public-ip wins; otherwise the
	// install-time persisted identity (process env or veil.env) is reused
	// so renewal certifies the same SANs instead of re-probing external
	// detection endpoints (#1186); only a bare "auto" reaches detection.
	ipSpec := strings.TrimSpace(publicIP)
	if !certFlagChanged(cmd, "public-ip") {
		if persisted := hostenv.EnvOrFile(envValues, "VEIL_PANEL_PUBLIC_IP"); persisted != "" {
			ipSpec = persisted
		}
	}
	var publicIPv4, publicIPv6 string
	switch {
	case ipSpec == "" || strings.EqualFold(ipSpec, "auto"):
		var err error
		publicIPv4, publicIPv6, err = acmeip.ResolvePublicIPs(ctx, "auto")
		if err != nil {
			return fmt.Errorf("detect public IP: %w", err)
		}
	default:
		var err error
		publicIPv4, publicIPv6, err = acmeip.ParsePublicIPSpec(ipSpec)
		if err != nil {
			return fmt.Errorf("public IP %q is not a valid IPv4/IPv6 spec or \"auto\"", ipSpec)
		}
	}

	// The persisted --le-ip-cert-port wins over the flag default so renewal
	// binds the same standalone port the install-time issuance used (#1189/#1186).
	if !certFlagChanged(cmd, "port") {
		if persisted := hostenv.EnvOrFile(envValues, "VEIL_PANEL_HTTP01_PORT"); persisted != "" {
			if parsed, err := strconv.Atoi(persisted); err == nil && parsed >= 0 && parsed <= 65535 {
				port = parsed
			}
		}
	}

	snapshot, hasSnapshot := loadCertSnapshot(etcDir, varDir)
	if strings.TrimSpace(email) == "" && hasSnapshot {
		email = snapshot.Settings.PanelEmail
		if email == "" {
			email = snapshot.Settings.Email
		}
	}
	viaCaddy := hasSnapshot && caddyassembly.PanelIPCertCaddyFronted(snapshot.Settings, snapshot.Inbounds)

	fence, release, err := certRenewFence(varDir)
	if err != nil {
		return err
	}
	if release != nil {
		defer release()
	}
	certPath := certTLSCertPath(envValues, etcDir)
	keyPath := certTLSKeyPath(envValues, certPath, etcDir)
	issuer := certRenewIssuer(privileged.DefaultSocketPath)
	result, err := issuer.IssueIPCert(ctx, privileged.IssueIPCertRequest{
		PublicIPv4: publicIPv4,
		PublicIPv6: publicIPv6,
		HTTPPort:   port,
		Email:      strings.TrimSpace(email),
		CertPath:   certPath,
		KeyPath:    keyPath,
		// Honor the controlled-CA knobs install persists into veil.env —
		// the process environment still wins for an explicit override
		// (#1189).
		CAServer:       hostenv.EnvOrFile(envValues, "VEIL_ACME_CA_URL"),
		Insecure:       hostenv.EnvOrFile(envValues, "VEIL_ACME_INSECURE") != "",
		CARoot:         hostenv.EnvOrFile(envValues, "VEIL_ACME_CA_ROOT"),
		HTTP01ViaCaddy: viaCaddy,
		// The panel itself consumes this certificate; its restart rides a
		// transient systemd timer so a running panel finishes in-flight
		// responses before reloadcmd restarts it (#1170).
		DeferPanelRestart: true,
		Fence:             fence,
	})
	if err != nil {
		return fmt.Errorf("renew panel IP certificate: %w", err)
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Issued certificate: %s\nKey: %s\n", result.CertPath, result.KeyPath)
	fmt.Fprintln(cmd.OutOrStdout(), "Panel restart scheduled (deferred) to load the renewed certificate.")
	return nil
}
