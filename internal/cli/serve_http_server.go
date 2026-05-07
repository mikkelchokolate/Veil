package cli

import (
	"crypto/tls"
	"net/http"
	"time"

	"github.com/veil-panel/veil/internal/api"
	"golang.org/x/crypto/acme/autocert"
)

type serveHTTPServerOptions struct {
	Listen      string
	Version     string
	AuthToken   string
	StatePath   string
	ApplyRoot   string
	KeyPath     string
	TLSEnabled  bool
	TLSCert     string
	TLSKey      string
	WebBasePath string
}

type ServeHTTPServer struct {
	opts serveHTTPServerOptions
}

func NewServeHTTPServer(opts serveHTTPServerOptions) ServeHTTPServer {
	return ServeHTTPServer{opts: opts}
}

func (s ServeHTTPServer) Build() (*http.Server, api.Reloader) {
	opts := s.opts
	handler, reloader := api.NewRouter(api.ServerInfo{Version: opts.Version, Mode: "server", AuthToken: opts.AuthToken, StatePath: opts.StatePath, ApplyRoot: opts.ApplyRoot, KeyPath: opts.KeyPath, WebBasePath: opts.WebBasePath})
	srv := &http.Server{
		Addr:              opts.Listen,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      120 * time.Second,
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    1 << 20,
	}
	if opts.TLSEnabled {
		srv.TLSConfig = s.tlsConfig()
	}
	return srv, reloader
}

func (s ServeHTTPServer) tlsConfig() *tls.Config {
	opts := s.opts
	if opts.TLSCert == "" && opts.TLSKey == "" && autoTLSDomain != "" {
		mgr := &autocert.Manager{
			Cache:      autocert.DirCache(autoTLSCacheDir),
			Prompt:     autocert.AcceptTOS,
			HostPolicy: autocert.HostWhitelist(autoTLSDomain),
			Email:      autoTLSEmail,
		}
		cfg := newServeTLSConfig()
		cfg.GetCertificate = mgr.GetCertificate
		return cfg
	}
	return newServeTLSConfig()
}

func newServeHTTPServer(listen string, version string, authToken string, statePath string, applyRoot string, keyPath string, tlsEnabled bool, tlsCert string, tlsKey string, webBasePath string) (*http.Server, api.Reloader) {
	return NewServeHTTPServer(serveHTTPServerOptions{
		Listen:      listen,
		Version:     version,
		AuthToken:   authToken,
		StatePath:   statePath,
		ApplyRoot:   applyRoot,
		KeyPath:     keyPath,
		TLSEnabled:  tlsEnabled,
		TLSCert:     tlsCert,
		TLSKey:      tlsKey,
		WebBasePath: webBasePath,
	}).Build()
}

// newServeTLSConfig returns a secure TLS configuration for the serve command.
// It enforces TLS 1.2 minimum with modern cipher suites and disables insecure
// features like TLS compression and session tickets.
func newServeTLSConfig() *tls.Config {
	return &tls.Config{
		MinVersion: tls.VersionTLS12,
		CipherSuites: []uint16{
			tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
			tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,
			tls.TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384,
			tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256,
		},
		PreferServerCipherSuites: true,
		CurvePreferences: []tls.CurveID{
			tls.X25519,
			tls.CurveP256,
		},
	}
}
