package cli

import (
	"context"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/mikkelchokolate/Veil/internal/acmeip"
	"github.com/mikkelchokolate/Veil/internal/apply"
	"github.com/mikkelchokolate/Veil/internal/hostenv"
	"github.com/mikkelchokolate/Veil/internal/privileged"
	"github.com/mikkelchokolate/Veil/internal/runtime"
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

	var statusJSON bool
	statusCmd := &cobra.Command{
		Use:   "status",
		Short: "Show the panel IP certificate state",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runCertStatus(cmd, statusJSON)
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
			return runCertRenew(cmd, renewPublicIP, renewEmail, renewPort)
		},
	}
	renewCmd.Flags().StringVar(&renewPublicIP, "public-ip", "auto", "public IPv4/IPv6 the certificate covers; auto detects both families")
	renewCmd.Flags().StringVar(&renewEmail, "email", "", "ACME account contact email (defaults to the panel settings email)")
	renewCmd.Flags().IntVar(&renewPort, "port", 80, "port used for Let's Encrypt HTTP-01 validation")
	cmd.AddCommand(renewCmd)
	return cmd
}

// certTLSCertPath resolves the panel TLS certificate path the same way the
// status flow does: VEIL_TLS_CERT first, then <etc>/panel/tls.crt.
func certTLSCertPath() string {
	if v := strings.TrimSpace(os.Getenv("VEIL_TLS_CERT")); v != "" {
		return v
	}
	return filepath.Join(hostenv.EtcDir(), "panel", "tls.crt")
}

func certTLSKeyPath(certPath string) string {
	if v := strings.TrimSpace(os.Getenv("VEIL_TLS_KEY")); v != "" {
		return v
	}
	if strings.TrimSpace(os.Getenv("VEIL_TLS_CERT")) != "" {
		return strings.TrimSuffix(certPath, filepath.Ext(certPath)) + ".key"
	}
	return filepath.Join(hostenv.EtcDir(), "panel", "tls.key")
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

func runCertStatus(cmd *cobra.Command, jsonOutput bool) error {
	certPath := certTLSCertPath()
	info := runtime.ReadTLSCert(certPath)
	view := certStatusView{
		TLSCertInfo:    info,
		KeyPath:        certTLSKeyPath(certPath),
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
func mintCertRenewFence() (privileged.FenceToken, func(), error) {
	databasePath := filepath.Join(hostenv.VarDir(), "veil.db")
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

func runCertRenew(cmd *cobra.Command, publicIP, email string, port int) error {
	ctx := cmd.Context()
	if ctx == nil {
		ctx = context.Background()
	}
	if ip := net.ParseIP(strings.TrimSpace(publicIP)); ip == nil && !strings.EqualFold(strings.TrimSpace(publicIP), "auto") && strings.TrimSpace(publicIP) != "" {
		return fmt.Errorf("--public-ip %q is not a valid IP address or \"auto\"", publicIP)
	}
	publicIPv4, publicIPv6, err := acmeip.ResolvePublicIPs(ctx, publicIP)
	if err != nil {
		return fmt.Errorf("detect public IP: %w", err)
	}
	fence, release, err := certRenewFence()
	if err != nil {
		return err
	}
	if release != nil {
		defer release()
	}
	certPath := certTLSCertPath()
	keyPath := certTLSKeyPath(certPath)
	issuer := certRenewIssuer(privileged.DefaultSocketPath)
	result, err := issuer.IssueIPCert(ctx, privileged.IssueIPCertRequest{
		PublicIPv4: publicIPv4,
		PublicIPv6: publicIPv6,
		HTTPPort:   port,
		Email:      strings.TrimSpace(email),
		CertPath:   certPath,
		KeyPath:    keyPath,
		// Honor the controlled-CA knobs install persists into veil.env.
		CAServer: strings.TrimSpace(os.Getenv("VEIL_ACME_CA_URL")),
		Insecure: strings.TrimSpace(os.Getenv("VEIL_ACME_INSECURE")) != "",
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
