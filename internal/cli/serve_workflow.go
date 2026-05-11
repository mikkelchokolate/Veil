package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

type serveWorkflowOptions struct {
	Version     string
	Listen      string
	AuthToken   string
	StatePath   string
	ApplyRoot   string
	KeyPath     string
	TLSCert     string
	TLSKey      string
	WebBasePath string
	AutoTLS     bool
	AutoTLSDir  string
}

func runServeWorkflow(cmd *cobra.Command, opts serveWorkflowOptions) error {
	cfg, err := NewServeSecurity(opts).Resolve()
	if err != nil {
		return err
	}
	server, stateReloader := NewServeHTTPServer(serveHTTPServerOptions{
		Listen:      cfg.Listen,
		Version:     opts.Version,
		AuthToken:   cfg.Token,
		StatePath:   cfg.StatePath,
		ApplyRoot:   cfg.ApplyRoot,
		KeyPath:     cfg.KeyPath,
		TLSEnabled:  cfg.TLSEnabled,
		TLSCert:     cfg.TLSCert,
		TLSKey:      cfg.TLSKey,
		PanelAccess: cfg.PanelAccess,
		Domain:      cfg.Domain,
		Email:       cfg.Email,
		WebBasePath: cfg.WebBasePath,
	}).Build()
	tlsLabel := "http"
	if cfg.TLSEnabled {
		tlsLabel = "https"
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Veil listening on %s://%s\n", tlsLabel, cfg.Listen)
	fmt.Fprintf(cmd.OutOrStdout(), "State path: %s (%s)\n", cfg.StatePath, cfg.StateSource)
	fmt.Fprintf(cmd.OutOrStdout(), "Apply root: %s (%s)\n", cfg.ApplyRoot, cfg.ApplyRootSource)
	fmt.Fprintf(cmd.OutOrStdout(), "Key path: %s (%s)\n", cfg.KeyPath, cfg.KeySource)
	if cfg.TLSEnabled {
		fmt.Fprintf(cmd.OutOrStdout(), "TLS: enabled (%s)\n", cfg.TLSSource)
	} else {
		fmt.Fprintln(cmd.OutOrStdout(), "TLS: disabled")
	}
	if cfg.WebBasePath != "/" {
		fmt.Fprintf(cmd.OutOrStdout(), "Web base path: %s\n", cfg.WebBasePath)
	}
	if cfg.TokenSource == "disabled" {
		fmt.Fprintln(cmd.OutOrStdout(), "API auth: disabled")
	} else {
		fmt.Fprintf(cmd.OutOrStdout(), "API auth: enabled (%s)\n", cfg.TokenSource)
	}

	return runServeLifecycle(cmd, server, stateReloader, cfg.TLSEnabled, cfg.TLSCert, cfg.TLSKey)
}
