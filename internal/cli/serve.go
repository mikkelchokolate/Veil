package cli

import (
	"time"

	"github.com/spf13/cobra"
)

const serveDrainTimeout = 5 * time.Second

func newServeCommand(version string) *cobra.Command {
	var listen string
	var authToken string
	var statePath string
	var applyRoot string
	var keyPath string
	var tlsCert string
	var tlsKey string
	var webBasePath string
	var autoTLS bool
	var autoTLSDir string
	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Run Veil HTTP API and web panel",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runServeWorkflow(cmd, serveWorkflowOptions{
				Version:     version,
				Listen:      listen,
				AuthToken:   authToken,
				StatePath:   statePath,
				ApplyRoot:   applyRoot,
				KeyPath:     keyPath,
				TLSCert:     tlsCert,
				TLSKey:      tlsKey,
				WebBasePath: webBasePath,
				AutoTLS:     autoTLS,
				AutoTLSDir:  autoTLSDir,
			})
		},
	}
	cmd.Flags().StringVar(&listen, "listen", "", "HTTP/HTTPS listen address; defaults to VEIL_LISTEN or 127.0.0.1:2096")
	cmd.Flags().StringVar(&authToken, "auth-token", "", "API bearer token; defaults to VEIL_API_TOKEN when set")
	cmd.Flags().StringVar(&statePath, "state", "", "management state JSON path; defaults to VEIL_STATE_PATH or /var/lib/veil/state.json")
	cmd.Flags().StringVar(&applyRoot, "apply-root", "", "root for staged apply files; defaults to VEIL_APPLY_ROOT or /etc/veil")
	cmd.Flags().StringVar(&keyPath, "key-path", "", "encryption key file path; defaults to VEIL_KEY_PATH or /etc/veil/state.key")
	cmd.Flags().StringVar(&tlsCert, "tls-cert", "", "TLS certificate file path; enables HTTPS when both --tls-cert and --tls-key are provided")
	cmd.Flags().StringVar(&tlsKey, "tls-key", "", "TLS private key file path; enables HTTPS when both --tls-cert and --tls-key are provided")
	cmd.Flags().StringVar(&webBasePath, "web-base-path", "", "base path prefix for the web panel (e.g. /secret/); defaults to VEIL_WEB_BASE_PATH or /")
	cmd.Flags().BoolVar(&autoTLS, "auto-tls", false, "auto-obtain Let's Encrypt TLS certificate using domain/email from state; requires state with domain and email set")
	cmd.Flags().StringVar(&autoTLSDir, "auto-tls-dir", "", "directory for auto-tls certificate cache; defaults to VEIL_AUTO_TLS_DIR or /var/lib/veil/autocert")
	return cmd
}

// Package-level variables set by resolveServeAutoTLS and used by newServeHTTPServer.
var autoTLSDomain, autoTLSEmail, autoTLSCacheDir string
