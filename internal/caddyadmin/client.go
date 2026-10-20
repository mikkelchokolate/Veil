package caddyadmin

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/mikkelchokolate/Veil/internal/runtimeports"
)

const defaultAdminTimeout = 30 * time.Second

type Client struct {
	AdminEndpoint string
	HTTPClient    *http.Client
}

func NewClient(endpoint string) Client {
	if endpoint == "" {
		endpoint = runtimeports.CaddyAdminEndpoint()
	}
	return Client{AdminEndpoint: endpoint, HTTPClient: &http.Client{Timeout: defaultAdminTimeout}}
}

// unixSocketPath reports whether endpoint uses Caddy's "unix/<path>" network
// address syntax and returns the socket path. A "|<mode>" permission suffix
// (valid in Caddy's admin.listen) is ignored: it only governs how Caddy
// creates the socket.
func unixSocketPath(endpoint string) (string, bool) {
	rest, ok := strings.CutPrefix(endpoint, "unix/")
	if !ok || rest == "" {
		return "", false
	}
	if i := strings.LastIndexByte(rest, '|'); i >= 0 {
		rest = rest[:i]
	}
	return rest, rest != ""
}

// requestBase resolves the URL prefix for admin requests and, for a Unix
// socket endpoint, rewires the client to dial that socket. The HTTP authority
// is 127.0.0.1: Caddy accepts only a loopback Host (or none) on unix-socket
// admin listeners (#1276).
func (c Client) requestBase(httpClient *http.Client) string {
	socket, ok := unixSocketPath(c.AdminEndpoint)
	if !ok {
		return c.AdminEndpoint
	}
	var transport *http.Transport
	if base, isTransport := httpClient.Transport.(*http.Transport); isTransport && base != nil {
		transport = base.Clone()
	} else {
		transport = http.DefaultTransport.(*http.Transport).Clone()
	}
	transport.Proxy = nil
	transport.DialContext = func(ctx context.Context, _, _ string) (net.Conn, error) {
		var dialer net.Dialer
		return dialer.DialContext(ctx, "unix", socket)
	}
	httpClient.Transport = transport
	return "http://127.0.0.1"
}

func (c Client) LoadConfig(configJSON []byte) error {
	return c.LoadConfigContext(context.Background(), configJSON)
}

func (c Client) LoadConfigContext(ctx context.Context, configJSON []byte) error {
	expectedDigest, err := canonicalConfigDigest(configJSON)
	if err != nil {
		return fmt.Errorf("caddy config is invalid JSON: %w", err)
	}
	httpClient := c.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: defaultAdminTimeout}
	}
	clientCopy := *httpClient
	clientCopy.CheckRedirect = func(_ *http.Request, _ []*http.Request) error {
		return http.ErrUseLastResponse
	}
	base := c.requestBase(&clientCopy)
	loadURL := base + "/load"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, loadURL, bytes.NewReader(configJSON))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := clientCopy.Do(req)
	if err != nil {
		return fmt.Errorf("caddy admin POST failed: %w", err)
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("caddy admin returned %s", resp.Status)
	}

	verifyReq, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/config/", nil)
	if err != nil {
		return err
	}
	active, err := clientCopy.Do(verifyReq)
	if err != nil {
		return fmt.Errorf("caddy active config verification failed: %w", err)
	}
	defer active.Body.Close()
	if active.StatusCode != http.StatusOK {
		return fmt.Errorf("caddy active config verification returned %s", active.Status)
	}
	body, err := io.ReadAll(io.LimitReader(active.Body, 4<<20+1))
	if err != nil {
		return fmt.Errorf("read caddy active config: %w", err)
	}
	if len(body) > 4<<20 {
		return fmt.Errorf("caddy active config exceeds verification limit")
	}
	activeDigest, err := canonicalConfigDigest(body)
	if err != nil {
		return fmt.Errorf("caddy active config is invalid JSON: %w", err)
	}
	if activeDigest != expectedDigest {
		return fmt.Errorf("caddy active config digest mismatch: expected %s, observed %s", expectedDigest, activeDigest)
	}
	return nil
}

func canonicalConfigDigest(body []byte) (string, error) {
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return "", err
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return "", fmt.Errorf("multiple JSON values")
		}
		return "", err
	}
	canonical, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(canonical)
	return hex.EncodeToString(sum[:]), nil
}
