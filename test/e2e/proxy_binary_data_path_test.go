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

	// 3. Start Mieru server (mita) using the generated config via control socket
	serverConfig := filepath.Join(srv.applyRoot, "generated", "mieru", "server_config.json")
	serverRuntime := t.TempDir()
	serverSocket := filepath.Join(serverRuntime, "mita.sock")
	serverPB := filepath.Join(serverRuntime, "server.conf.pb")
	serverLogPath := filepath.Join(serverRuntime, "mita.log")
	serverLog, err := os.Create(serverLogPath)
	if err != nil {
		t.Fatal(err)
	}
	defer serverLog.Close()

	serverEnv := append(os.Environ(),
		"MITA_CONFIG_FILE="+serverPB,
		"MITA_UDS_PATH="+serverSocket,
		"MITA_INSECURE_UDS=1",
		"MITA_LOG_NO_TIMESTAMP=true",
	)
	cmdServer := exec.Command(mitaPath, "run")
	cmdServer.Env = serverEnv
	cmdServer.Stdout = serverLog
	cmdServer.Stderr = serverLog
	if err := cmdServer.Start(); err != nil {
		t.Fatalf("start mita daemon: %v", err)
	}
	defer func() {
		if cmdServer.Process != nil {
			_ = cmdServer.Process.Kill()
		}
	}()
	if err := waitUnixSocket(serverSocket, 15*time.Second); err != nil {
		logBytes, _ := os.ReadFile(serverLogPath)
		t.Fatalf("mita control socket did not appear: %v\nlog:\n%s", err, logBytes)
	}
	runMieruControl(t, serverEnv, mitaPath, serverLogPath, "apply", "config", serverConfig)
	runMieruControl(t, serverEnv, mitaPath, serverLogPath, "start")

	// 4. Retrieve client configuration
	resp = srv.do(http.MethodGet, "/api/client-links", "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("client links expected 200, got %d", resp.StatusCode)
	}
	linksBody := readJSON(t, resp)
	artifactsRaw, _ := json.Marshal(linksBody["artifacts"])
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

	profiles, ok := clientMap["profiles"].([]any)
	if !ok || len(profiles) == 0 {
		t.Fatalf("Mieru config has no profiles: %s", clientConfigJSON)
	}
	profile, ok := profiles[0].(map[string]any)
	if !ok {
		t.Fatalf("Mieru profile has unexpected shape: %T", profiles[0])
	}
	servers, ok := profile["servers"].([]any)
	if !ok || len(servers) == 0 {
		t.Fatalf("Mieru profile has no servers: %s", clientConfigJSON)
	}
	server, ok := servers[0].(map[string]any)
	if !ok {
		t.Fatalf("Mieru server has unexpected shape: %T", servers[0])
	}
	delete(server, "domainName")
	server["ipAddress"] = "127.0.0.1"

	modifiedClientJSON, err := json.Marshal(clientMap)
	if err != nil {
		t.Fatalf("marshal modified client config: %v", err)
	}

	tempClientFile := filepath.Join(t.TempDir(), "client.json")
	if err := os.WriteFile(tempClientFile, modifiedClientJSON, 0o600); err != nil {
		t.Fatalf("write client config: %v", err)
	}

	// 6. Start Mieru client
	clientLogPath := filepath.Join(t.TempDir(), "mieru.log")
	clientLog, err := os.Create(clientLogPath)
	if err != nil {
		t.Fatal(err)
	}
	defer clientLog.Close()

	cmdClient := exec.Command(mieruPath, "run")
	cmdClient.Env = append(os.Environ(),
		"MIERU_CONFIG_JSON_FILE="+tempClientFile,
		"MIERU_LOG_NO_TIMESTAMP=true",
	)
	cmdClient.Stdout = clientLog
	cmdClient.Stderr = clientLog
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
	if err := waitListen(socksAddr, 15*time.Second); err != nil {
		serverBytes, _ := os.ReadFile(serverLogPath)
		clientBytes, _ := os.ReadFile(clientLogPath)
		t.Fatalf("mieru client did not listen: %v\nserver log:\n%s\nclient log:\n%s", err, serverBytes, clientBytes)
	}

	assertHTTPThroughSOCKS(t, socksAddr, backend.URL, expectedResponse)
}

// TestHysteria2DataPath tests data flow through a real Hysteria2 server/client if Hysteria binary is installed.
func TestHysteria2DataPath(t *testing.T) {
	hysteriaPath, err := exec.LookPath("hysteria")
	if err != nil {
		t.Skip("hysteria binary not found in PATH, skipping data-path test")
	}
	testHysteria2DataPath(t, hysteriaPath)
}

func testHysteria2DataPath(t *testing.T, hysteriaPath string) {
	t.Helper()

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
		applyResponse := readJSON(t, resp)
		stateResponse := readJSON(t, srv.do(http.MethodGet, "/api/apply/state", ""))
		jobsResponse := readJSON(t, srv.do(http.MethodGet, "/api/apply/jobs", ""))
		t.Fatalf("apply expected 200, got %d: %v\napply state: %v\napply jobs: %v\nserver log:\n%s", resp.StatusCode, applyResponse, stateResponse, jobsResponse, srv.logBuf.String())
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

	// Replace the generated TLS section with our self-signed certificate.
	// The renderer currently emits a tls: block referencing the panel cert;
	// older versions emitted an acme: block.
	var modifiedYAML string
	if strings.Contains(string(yamlContent), "\ntls:") {
		re := regexp.MustCompile(`(?s)\ntls:\n\s+cert:.*?\n\s+key:.*?\n`)
		modifiedYAML = re.ReplaceAllString(string(yamlContent), fmt.Sprintf("\ntls:\n  cert: %s\n  key: %s\n", filepath.ToSlash(certPath), filepath.ToSlash(keyPath)))
	} else {
		re := regexp.MustCompile(`(?s)acme:\s+domains:\s+-\s+\S+`)
		modifiedYAML = re.ReplaceAllString(string(yamlContent), fmt.Sprintf("tls:\n  cert: %s\n  key: %s", filepath.ToSlash(certPath), filepath.ToSlash(keyPath)))
	}

	tempServerYAML := filepath.Join(tempDir, "server.yaml")
	if err := os.WriteFile(tempServerYAML, []byte(modifiedYAML), 0o600); err != nil {
		t.Fatalf("write modified server config: %v", err)
	}

	// 4. Start Hysteria2 server
	serverLog := filepath.Join(tempDir, "server.log")
	serverLogFile, err := os.Create(serverLog)
	if err != nil {
		t.Fatalf("create server log: %v", err)
	}
	defer serverLogFile.Close()
	cmdServer := exec.Command(hysteriaPath, "server", "--config", tempServerYAML)
	cmdServer.Stdout = serverLogFile
	cmdServer.Stderr = serverLogFile
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
	clientLog := filepath.Join(tempDir, "client.log")
	clientLogFile, err := os.Create(clientLog)
	if err != nil {
		t.Fatalf("create client log: %v", err)
	}
	defer clientLogFile.Close()
	cmdClient := exec.Command(hysteriaPath, "client", "-c", tempClientYAML)
	cmdClient.Stdout = clientLogFile
	cmdClient.Stderr = clientLogFile
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
		cb, _ := os.ReadFile(clientLog)
		sb, _ := os.ReadFile(serverLog)
		yb, _ := os.ReadFile(tempServerYAML)
		t.Fatalf("hysteria client SOCKS5 did not listen: %v\nserver yaml:\n%s\nserver log:\n%s\nclient log:\n%s", err, string(yb), string(sb), string(cb))
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

// TestNaiveProxyDataPath tests data flow through a real Caddy/NaiveProxy
// server/client when the optional client binary is installed. The strict CI
// variant calls the same implementation but requires both binaries.
func TestNaiveProxyDataPath(t *testing.T) {
	caddyPath, err := exec.LookPath("caddy")
	if err != nil {
		t.Skip("caddy binary not found in PATH, skipping data-path test")
	}
	naivePath, err := exec.LookPath("naive")
	if err != nil {
		t.Skip("naive binary not found in PATH, skipping data-path test")
	}
	testNaiveProxyDataPath(t, caddyPath, naivePath)
}

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
