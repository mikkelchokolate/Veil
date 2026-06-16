//go:build e2e

package e2e

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"golang.org/x/net/proxy"
)

// generateSelfSignedCert generates a self-signed TLS cert and key on disk.
func generateSelfSignedCert(certPath, keyPath string) error {
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return err
	}

	notBefore := time.Now()
	notAfter := notBefore.Add(365 * 24 * time.Hour)

	serialNumberLimit := new(big.Int).Lsh(big.NewInt(1), 128)
	serialNumber, err := rand.Int(rand.Reader, serialNumberLimit)
	if err != nil {
		return err
	}

	template := x509.Certificate{
		SerialNumber: serialNumber,
		Subject: pkix.Name{
			Organization: []string{"Veil E2E Test"},
		},
		NotBefore:             notBefore,
		NotAfter:              notAfter,
		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		IPAddresses:           []net.IP{net.ParseIP("127.0.0.1")},
		DNSNames:              []string{"localhost"},
	}

	derBytes, err := x509.CreateCertificate(rand.Reader, &template, &template, &priv.PublicKey, priv)
	if err != nil {
		return err
	}

	certOut, err := os.Create(certPath)
	if err != nil {
		return err
	}
	defer certOut.Close()
	if err := pem.Encode(certOut, &pem.Block{Type: "CERTIFICATE", Bytes: derBytes}); err != nil {
		return err
	}

	keyOut, err := os.OpenFile(keyPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	defer keyOut.Close()
	privBytes, err := x509.MarshalECPrivateKey(priv)
	if err != nil {
		return err
	}
	if err := pem.Encode(keyOut, &pem.Block{Type: "EC PRIVATE KEY", Bytes: privBytes}); err != nil {
		return err
	}

	return nil
}

// TestMieruDataPath tests data flow through a real Mieru server/client if mita/mieru binaries are installed.
func TestMieruDataPath(t *testing.T) {
	mitaPath, err := exec.LookPath("mita")
	if err != nil {
		t.Skip("mita binary not found in PATH, skipping data-path test")
	}

	mieruPath, err := exec.LookPath("mieru")
	if err != nil {
		t.Skip("mieru binary not found in PATH, skipping data-path test")
	}

	// 1. Start backend HTTP server
	expectedResponse := "hello from backend"
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(expectedResponse))
	}))
	defer backend.Close()

	// 2. Start Veil serving panel
	srv := startServer(t, serverOptions{token: "e2e-secret-token"})

	// Configure settings and inbound
	inboundPort := freePort(t)
	resp := srv.do(http.MethodPut, "/api/settings", `{"panelListen":"127.0.0.1:2096","mode":"dev","domain":"vpn.example.com"}`)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("settings expected 200, got %d: %v", resp.StatusCode, readJSON(t, resp))
	}
	drain(resp)

	resp = srv.do(http.MethodPost, "/api/inbounds", fmt.Sprintf(`{"name":"mieru-tcp","protocol":"mieru","transport":"tcp","port":%d,"enabled":true,"profiles":[{"name":"alice","password":"alice-pass","enabled":true}]}`, inboundPort))
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("inbound expected 201, got %d: %v", resp.StatusCode, readJSON(t, resp))
	}
	drain(resp)

	// Apply settings
	resp = srv.do(http.MethodPost, "/api/apply/plan", "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("apply plan expected 200, got %d: %v", resp.StatusCode, readJSON(t, resp))
	}
	drain(resp)

	resp = srv.do(http.MethodPost, "/api/apply", `{"confirm":true}`)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("apply expected 200, got %d: %v", resp.StatusCode, readJSON(t, resp))
	}
	drain(resp)

	// 3. Start Mieru server (mita) using the generated config
	serverConfig := filepath.Join(srv.applyRoot, "generated", "mieru", "server_config.json")
	// mita run uses MITA_CONFIG_JSON_FILE env var, needs /var/run/mita dir
	if err := os.MkdirAll("/var/run/mita", 0755); err != nil {
		t.Fatalf("create /var/run/mita: %v", err)
	}
	cmdServer := exec.Command(mitaPath, "run")
	cmdServer.Env = append(os.Environ(), "MITA_CONFIG_JSON_FILE="+serverConfig)
	if err := cmdServer.Start(); err != nil {
		t.Fatalf("start mita server: %v", err)
	}
	defer func() {
		if cmdServer.Process != nil {
			_ = cmdServer.Process.Kill()
		}
	}()

	// 4. Retrieve client configuration
	resp = srv.do(http.MethodGet, "/api/client-links", "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("client links expected 200, got %d", resp.StatusCode)
	}
	linksBody := readJSON(t, resp)
	artifactsRaw, _ := json.Marshal(linksBody["links"])
	var artifacts []struct {
		Protocol string `json:"protocol"`
		Kind     string `json:"kind"`
		Content  string `json:"content"`
	}
	if err := json.Unmarshal(artifactsRaw, &artifacts); err != nil {
		t.Fatalf("decode artifacts: %v", err)
	}

	var clientConfigJSON string
	for _, art := range artifacts {
		if art.Protocol == "mieru" && art.Kind == "client_config" {
			clientConfigJSON = art.Content
			break
		}
	}
	if clientConfigJSON == "" {
		t.Fatal("mieru client config artifact not found")
	}

	// 5. Modify client configuration to listen locally and connect locally
	var clientMap map[string]any
	if err := json.Unmarshal([]byte(clientConfigJSON), &clientMap); err != nil {
		t.Fatalf("unmarshal client config: %v", err)
	}

	socksPort := freePort(t)
	clientMap["socks5Port"] = socksPort
	clientMap["socks5ListenLAN"] = false

	servers, ok := clientMap["servers"].([]any)
	if ok && len(servers) > 0 {
		server, ok := servers[0].(map[string]any)
		if ok {
			server["domainName"] = "127.0.0.1"
		}
	}

	modifiedClientJSON, err := json.Marshal(clientMap)
	if err != nil {
		t.Fatalf("marshal modified client config: %v", err)
	}

	tempClientFile := filepath.Join(t.TempDir(), "client.json")
	if err := os.WriteFile(tempClientFile, modifiedClientJSON, 0o600); err != nil {
		t.Fatalf("write client config: %v", err)
	}

	// 6. Start Mieru client
	// mieru run uses MIERU_CONFIG_JSON_FILE env var, needs /var/run/mieru dir
	if err := os.MkdirAll("/var/run/mieru", 0755); err != nil {
		t.Fatalf("create /var/run/mieru: %v", err)
	}
	cmdClient := exec.Command(mieruPath, "run")
	cmdClient.Env = append(os.Environ(), "MIERU_CONFIG_JSON_FILE="+tempClientFile)
	if err := cmdClient.Start(); err != nil {
		t.Fatalf("start mieru client: %v", err)
	}
	defer func() {
		if cmdClient.Process != nil {
			_ = cmdClient.Process.Kill()
		}
	}()

	// Wait for Mieru client SOCKS5 to start listening
	socksAddr := fmt.Sprintf("127.0.0.1:%d", socksPort)
	if err := waitListen(socksAddr, 5*time.Second); err != nil {
		t.Fatalf("mieru client SOCKS5 did not listen: %v", err)
	}

	// 7. Test proxying through client SOCKS5
	dialer, err := proxy.SOCKS5("tcp", socksAddr, nil, proxy.Direct)
	if err != nil {
		t.Fatalf("create SOCKS5 dialer: %v", err)
	}

	httpClient := &http.Client{
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
				return dialer.Dial(network, addr)
			},
		},
		Timeout: 5 * time.Second,
	}

	res, err := httpClient.Get(backend.URL)
	if err != nil {
		t.Fatalf("GET request failed: %v", err)
	}
	defer res.Body.Close()

	body, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatalf("read response body: %v", err)
	}

	if string(body) != expectedResponse {
		t.Fatalf("expected response %q, got %q", expectedResponse, string(body))
	}
}

// TestHysteria2DataPath tests data flow through a real Hysteria2 server/client if Hysteria binary is installed.
func TestHysteria2DataPath(t *testing.T) {
	hysteriaPath, err := exec.LookPath("hysteria")
	if err != nil {
		t.Skip("hysteria binary not found in PATH, skipping data-path test")
	}

	// 1. Start backend HTTP server
	expectedResponse := "hello from hysteria2"
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(expectedResponse))
	}))
	defer backend.Close()

	// 2. Start Veil serving panel
	srv := startServer(t, serverOptions{token: "e2e-secret-token"})

	// Configure settings and inbound
	inboundPort := freePort(t)
	resp := srv.do(http.MethodPut, "/api/settings", `{"panelListen":"127.0.0.1:2096","mode":"dev","domain":"vpn.example.com"}`)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("settings expected 200, got %d: %v", resp.StatusCode, readJSON(t, resp))
	}
	drain(resp)

	resp = srv.do(http.MethodPost, "/api/inbounds", fmt.Sprintf(`{"name":"hy2-udp","protocol":"hysteria2","transport":"udp","port":%d,"enabled":true,"password":"secret-pass"}`, inboundPort))
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("inbound expected 201, got %d: %v", resp.StatusCode, readJSON(t, resp))
	}
	drain(resp)

	// Apply settings
	resp = srv.do(http.MethodPost, "/api/apply/plan", "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("apply plan expected 200, got %d: %v", resp.StatusCode, readJSON(t, resp))
	}
	drain(resp)

	resp = srv.do(http.MethodPost, "/api/apply", `{"confirm":true}`)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("apply expected 200, got %d: %v", resp.StatusCode, readJSON(t, resp))
	}
	drain(resp)

	// 3. Modify generated Hysteria2 server config to use self-signed TLS cert
	serverConfig := filepath.Join(srv.applyRoot, "generated", "hysteria2", "hy2-udp.yaml")
	yamlContent, err := os.ReadFile(serverConfig)
	if err != nil {
		t.Fatalf("read server yaml: %v", err)
	}

	tempDir := t.TempDir()
	certPath := filepath.Join(tempDir, "cert.pem")
	keyPath := filepath.Join(tempDir, "key.pem")
	if err := generateSelfSignedCert(certPath, keyPath); err != nil {
		t.Fatalf("generate self signed cert: %v", err)
	}

	// Remove acme: section and replace with tls:
	re := regexp.MustCompile(`(?s)acme:\s+domains:\s+-\s+\S+`)
	modifiedYAML := re.ReplaceAllString(string(yamlContent), fmt.Sprintf("tls:\n  cert: %s\n  key: %s", filepath.ToSlash(certPath), filepath.ToSlash(keyPath)))

	tempServerYAML := filepath.Join(tempDir, "server.yaml")
	if err := os.WriteFile(tempServerYAML, []byte(modifiedYAML), 0o600); err != nil {
		t.Fatalf("write modified server config: %v", err)
	}

	// 4. Start Hysteria2 server
	cmdServer := exec.Command(hysteriaPath, "server", "--config", tempServerYAML)
	if err := cmdServer.Start(); err != nil {
		t.Fatalf("start hysteria server: %v", err)
	}
	defer func() {
		if cmdServer.Process != nil {
			_ = cmdServer.Process.Kill()
		}
	}()

	// 5. Get client links
	resp = srv.do(http.MethodGet, "/api/client-links", "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("client links expected 200, got %d", resp.StatusCode)
	}
	linksBody := readJSON(t, resp)
	artifactsRaw, _ := json.Marshal(linksBody["links"])
	var artifacts []struct {
		Name     string `json:"name"`
		Protocol string `json:"protocol"`
		Kind     string `json:"kind"`
		URI      string `json:"uri"`
	}
	if err := json.Unmarshal(artifactsRaw, &artifacts); err != nil {
		t.Fatalf("decode artifacts: %v", err)
	}

	var uri string
	for _, art := range artifacts {
		if art.Protocol == "hysteria2" {
			uri = art.URI
			break
		}
	}
	if uri == "" {
		t.Fatal("hysteria2 client URI not found")
	}

	// 6. Parse URI and create client configuration YAML
	parsed, err := url.Parse(uri)
	if err != nil {
		t.Fatalf("parse URI: %v", err)
	}
	auth := parsed.User.String()
	socksPort := freePort(t)

	clientYAML := fmt.Sprintf(`server: 127.0.0.1:%d
auth: %s
tls:
  insecure: true
socks5:
  listen: 127.0.0.1:%d
`, inboundPort, auth, socksPort)

	tempClientYAML := filepath.Join(tempDir, "client.yaml")
	if err := os.WriteFile(tempClientYAML, []byte(clientYAML), 0o600); err != nil {
		t.Fatalf("write client YAML: %v", err)
	}

	// 7. Start Hysteria2 client
	cmdClient := exec.Command(hysteriaPath, "client", "-c", tempClientYAML)
	if err := cmdClient.Start(); err != nil {
		t.Fatalf("start hysteria client: %v", err)
	}
	defer func() {
		if cmdClient.Process != nil {
			_ = cmdClient.Process.Kill()
		}
	}()

	// Wait for client SOCKS5 to start listening
	socksAddr := fmt.Sprintf("127.0.0.1:%d", socksPort)
	if err := waitListen(socksAddr, 5*time.Second); err != nil {
		t.Fatalf("hysteria client SOCKS5 did not listen: %v", err)
	}

	// 8. Test proxying through client SOCKS5
	dialer, err := proxy.SOCKS5("tcp", socksAddr, nil, proxy.Direct)
	if err != nil {
		t.Fatalf("create SOCKS5 dialer: %v", err)
	}

	httpClient := &http.Client{
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
				return dialer.Dial(network, addr)
			},
		},
		Timeout: 5 * time.Second,
	}

	res, err := httpClient.Get(backend.URL)
	if err != nil {
		t.Fatalf("GET request failed: %v", err)
	}
	defer res.Body.Close()

	body, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatalf("read response body: %v", err)
	}

	if string(body) != expectedResponse {
		t.Fatalf("expected response %q, got %q", expectedResponse, string(body))
	}
}

// TestNaiveProxyDataPath tests data flow through a real Caddy/NaiveProxy server/client if caddy and naive binaries are installed.
func TestNaiveProxyDataPath(t *testing.T) {
	caddyPath, err := exec.LookPath("caddy")
	if err != nil {
		t.Skip("caddy binary not found in PATH, skipping data-path test")
	}
	naivePath, err := exec.LookPath("naive")
	if err != nil {
		t.Skip("naive binary not found in PATH, skipping data-path test")
	}

	// 1. Start backend HTTP server
	expectedResponse := "hello from naiveproxy"
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(expectedResponse))
	}))
	defer backend.Close()

	// 2. Start Veil serving panel
	srv := startServer(t, serverOptions{token: "e2e-secret-token"})

	// Configure settings and inbound
	inboundPort := freePort(t)
	resp := srv.do(http.MethodPut, "/api/settings", `{"panelListen":"127.0.0.1:2096","mode":"dev","domain":"vpn.example.com"}`)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("settings expected 200, got %d: %v", resp.StatusCode, readJSON(t, resp))
	}
	drain(resp)

	resp = srv.do(http.MethodPost, "/api/inbounds", fmt.Sprintf(`{"name":"naive-tcp","protocol":"naiveproxy","transport":"tcp","port":%d,"enabled":true,"password":"secret-pass"}`, inboundPort))
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("inbound expected 201, got %d: %v", resp.StatusCode, readJSON(t, resp))
	}
	drain(resp)

	// Apply settings
	resp = srv.do(http.MethodPost, "/api/apply/plan", "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("apply plan expected 200, got %d: %v", resp.StatusCode, readJSON(t, resp))
	}
	drain(resp)

	resp = srv.do(http.MethodPost, "/api/apply", `{"confirm":true}`)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("apply expected 200, got %d: %v", resp.StatusCode, readJSON(t, resp))
	}
	drain(resp)

	// 3. Modify generated Caddyfile to run over HTTP (remove domain name and tls directives)
	serverConfig := filepath.Join(srv.applyRoot, "generated", "caddy", "naive-tcp.Caddyfile")
	caddyfileContent, err := os.ReadFile(serverConfig)
	if err != nil {
		t.Fatalf("read caddyfile: %v", err)
	}

	// Change format: ":port, vpn.example.com {" -> "127.0.0.1:port {"
	caddyfile := string(caddyfileContent)
	caddyfile = strings.Replace(caddyfile, fmt.Sprintf(":%d, vpn.example.com", inboundPort), fmt.Sprintf("127.0.0.1:%d", inboundPort), 1)

	// Remove tls directive
	reTls := regexp.MustCompile(`(?m)^\s*tls\s+\S+\s*$`)
	caddyfile = reTls.ReplaceAllString(caddyfile, "")

	tempDir := t.TempDir()
	tempCaddyfile := filepath.Join(tempDir, "Caddyfile")
	if err := os.WriteFile(tempCaddyfile, []byte(caddyfile), 0o600); err != nil {
		t.Fatalf("write modified caddyfile: %v", err)
	}

	// 4. Start Caddy server
	cmdServer := exec.Command(caddyPath, "run", "--config", tempCaddyfile, "--adapter", "caddyfile")
	if err := cmdServer.Start(); err != nil {
		t.Fatalf("start caddy server: %v", err)
	}
	defer func() {
		if cmdServer.Process != nil {
			_ = cmdServer.Process.Kill()
		}
	}()

	// Wait for Caddy server to listen
	serverAddr := fmt.Sprintf("127.0.0.1:%d", inboundPort)
	if err := waitListen(serverAddr, 5*time.Second); err != nil {
		t.Fatalf("caddy server did not listen: %v", err)
	}

	// 5. Get client links
	resp = srv.do(http.MethodGet, "/api/client-links", "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("client links expected 200, got %d", resp.StatusCode)
	}
	linksBody := readJSON(t, resp)
	artifactsRaw, _ := json.Marshal(linksBody["links"])
	var artifacts []struct {
		Name     string `json:"name"`
		Protocol string `json:"protocol"`
		Kind     string `json:"kind"`
		URI      string `json:"uri"`
	}
	if err := json.Unmarshal(artifactsRaw, &artifacts); err != nil {
		t.Fatalf("decode artifacts: %v", err)
	}

	var uri string
	for _, art := range artifacts {
		if art.Protocol == "naiveproxy" {
			uri = art.URI
			break
		}
	}
	if uri == "" {
		t.Fatal("naiveproxy client URI not found")
	}

	// 6. Build client configuration JSON using HTTP
	parsed, err := url.Parse(uri)
	if err != nil {
		t.Fatalf("parse URI: %v", err)
	}
	username := parsed.User.Username()
	password, _ := parsed.User.Password()
	socksPort := freePort(t)

	// Since we are running Caddy over plain HTTP to avoid cert validation,
	// we configure the proxy using http://
	clientConfig := map[string]any{
		"listen":  fmt.Sprintf("socks://127.0.0.1:%d", socksPort),
		"proxy":   fmt.Sprintf("http://%s:%s@127.0.0.1:%d", username, password, inboundPort),
		"padding": true,
	}

	clientJSON, err := json.Marshal(clientConfig)
	if err != nil {
		t.Fatalf("marshal client config: %v", err)
	}

	tempClientJSON := filepath.Join(tempDir, "client.json")
	if err := os.WriteFile(tempClientJSON, clientJSON, 0o600); err != nil {
		t.Fatalf("write client JSON: %v", err)
	}

	// 7. Start naive client
	cmdClient := exec.Command(naivePath, tempClientJSON)
	if err := cmdClient.Start(); err != nil {
		t.Fatalf("start naive client: %v", err)
	}
	defer func() {
		if cmdClient.Process != nil {
			_ = cmdClient.Process.Kill()
		}
	}()

	// Wait for client SOCKS5 to start listening
	socksAddr := fmt.Sprintf("127.0.0.1:%d", socksPort)
	if err := waitListen(socksAddr, 5*time.Second); err != nil {
		t.Fatalf("naive client SOCKS5 did not listen: %v", err)
	}

	// 8. Test proxying through client SOCKS5
	dialer, err := proxy.SOCKS5("tcp", socksAddr, nil, proxy.Direct)
	if err != nil {
		t.Fatalf("create SOCKS5 dialer: %v", err)
	}

	httpClient := &http.Client{
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
				return dialer.Dial(network, addr)
			},
		},
		Timeout: 5 * time.Second,
	}

	res, err := httpClient.Get(backend.URL)
	if err != nil {
		t.Fatalf("GET request failed: %v", err)
	}
	defer res.Body.Close()

	body, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatalf("read response body: %v", err)
	}

	if string(body) != expectedResponse {
		t.Fatalf("expected response %q, got %q", expectedResponse, string(body))
	}
}

// waitListen helper checks if the port starts listening within the duration
func waitListen(addr string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("tcp", addr, 100*time.Millisecond)
		if err == nil {
			conn.Close()
			return nil
		}
		time.Sleep(50 * time.Millisecond)
	}
	return fmt.Errorf("port %s did not listen within %v", addr, timeout)
}
