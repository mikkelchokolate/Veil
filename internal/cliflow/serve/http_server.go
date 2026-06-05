package serve

import (
	"crypto/tls"
	"net/http"
	"time"

	"github.com/mikkelchokolate/Veil/internal/api"
	"golang.org/x/crypto/acme/autocert"
)

type HTTPServerOptions struct {
	Listen              string
	Version             string
	AuthToken           string
	PublicListen        bool
	MetricsAuthRequired bool
	StatePath           string
	ApplyRoot           string
	KeyPath             string
	TLSEnabled          bool
	TLSCert             string
	TLSKey              string
	AutoTLSDomain       string
	AutoTLSEmail        string
	AutoTLSCacheDir     string
	PanelAccess         string
	Domain              string
	Email               string
	WebBasePath         string
	SetupAllowed        bool
}

type HTTPServer struct {
	opts HTTPServerOptions
}

func NewHTTPServer(opts HTTPServerOptions) HTTPServer {
	return HTTPServer{opts: opts}
}

func (s HTTPServer) Build() (*http.Server, api.Reloader) {
	opts := s.opts
	handler, reloader := api.NewRouter(api.ServerInfo{Version: opts.Version, Mode: "server", AuthToken: opts.AuthToken, PublicListen: opts.PublicListen, MetricsAuthRequired: opts.MetricsAuthRequired, StatePath: opts.StatePath, ApplyRoot: opts.ApplyRoot, KeyPath: opts.KeyPath, PanelListen: opts.Listen, PanelAccess: opts.PanelAccess, Domain: opts.Domain, Email: opts.Email, WebBasePath: opts.WebBasePath, SetupAllowed: opts.SetupAllowed})
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

func (s HTTPServer) tlsConfig() *tls.Config {
	opts := s.opts
	if opts.TLSCert == "" && opts.TLSKey == "" && opts.AutoTLSDomain != "" {
		mgr := &autocert.Manager{
			Cache:      autocert.DirCache(opts.AutoTLSCacheDir),
			Prompt:     autocert.AcceptTOS,
			HostPolicy: autocert.HostWhitelist(opts.AutoTLSDomain),
			Email:      opts.AutoTLSEmail,
		}
		cfg := NewTLSConfig()
		cfg.GetCertificate = mgr.GetCertificate
		return cfg
	}
	return NewTLSConfig()
}

// NewTLSConfig returns a secure TLS configuration for the serve command.
// It enforces TLS 1.2 minimum with modern cipher suites and disables insecure
// features like TLS compression and session tickets.
func NewTLSConfig() *tls.Config {
	return &tls.Config{
		MinVersion: tls.VersionTLS12,
		CipherSuites: []uint16{
			tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
			tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,
			tls.TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384,
			tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256,
		},
		CurvePreferences: []tls.CurveID{
			tls.X25519,
			tls.CurveP256,
		},
	}
}
