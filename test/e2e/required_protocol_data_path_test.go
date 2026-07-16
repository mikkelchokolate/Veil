//go:build e2e

package e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
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

func requiredBinary(t *testing.T, name string) string {
	t.Helper()
	path, err := exec.LookPath(name)
	if err != nil {
		t.Fatalf("required real protocol binary %q is not installed: %v", name, err)
	}
	return path
}

func TestRequiredMieruTCPDataPath(t *testing.T) {
	testRequiredMieruDataPath(t, "tcp")
}

func TestRequiredMieruUDPDataPath(t *testing.T) {
	testRequiredMieruDataPath(t, "udp")
}

func testRequiredMieruDataPath(t *testing.T, transport string) {
	t.Helper()
	mitaPath := requiredBinary(t, "mita")
	mieruPath := requiredBinary(t, "mieru")

	expectedResponse := "hello from mieru over " + transport
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(expectedResponse))
	}))
	defer backend.Close()

	srv := startServer(t, serverOptions{token: "e2e-secret-token"})
	inboundPort := freePort(t)
	resp := srv.do(http.MethodPut, "/api/settings", `{"panelListen":"127.0.0.1:2096","mode":"dev","domain":"127.0.0.1"}`)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("settings expected 200, got %d: %v", resp.StatusCode, readJSON(t, resp))
	}
	drain(resp)

	body := fmt.Sprintf(`{"name":"mieru-%s","protocol":"mieru","transport":%q,"port":%d,"enabled":true,"profiles":[{"name":"alice","password":"alice-pass","enabled":true}]}`, transport, transport, inboundPort)
	resp = srv.do(http.MethodPost, "/api/inbounds", body)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("inbound expected 201, got %d: %v", resp.StatusCode, readJSON(t, resp))
	}
	drain(resp)

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

	serverConfig := filepath.Join(srv.applyRoot, "generated", "mieru", "server_config.json")
	serverLogPath := filepath.Join(t.TempDir(), "mita.log")
	serverLog, err := os.Create(serverLogPath)
	if err != nil {
		t.Fatal(err)
	}
	defer serverLog.Close()

	cmdServer := exec.Command(mitaPath, "run")
	cmdServer.Env = append(os.Environ(), "MITA_CONFIG_JSON_FILE="+serverConfig)
	cmdServer.Stdout = serverLog
	cmdServer.Stderr = serverLog
	if err := cmdServer.Start(); err != nil {
		t.Fatalf("start mita: %v", err)
	}
	defer func() { _ = cmdServer.Process.Kill() }()

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
	for _, artifact := range artifacts {
		if artifact.Protocol == "mieru" && artifact.Kind == "client_config" {
			clientConfigJSON = artifact.Content
			break
		}
	}
	if clientConfigJSON == "" {
		t.Fatal("panel did not return a Mieru client configuration")
	}

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
		t.Fatal(err)
	}
	clientFile := filepath.Join(t.TempDir(), "client.json")
	if err := os.WriteFile(clientFile, modifiedClientJSON, 0o600); err != nil {
		t.Fatal(err)
	}
	clientLogPath := filepath.Join(t.TempDir(), "mieru.log")
	clientLog, err := os.Create(clientLogPath)
	if err != nil {
		t.Fatal(err)
	}
	defer clientLog.Close()
	cmdClient := exec.Command(mieruPath, "run")
	cmdClient.Env = append(os.Environ(), "MIERU_CONFIG_JSON_FILE="+clientFile)
	cmdClient.Stdout = clientLog
	cmdClient.Stderr = clientLog
	if err := cmdClient.Start(); err != nil {
		t.Fatalf("start mieru: %v", err)
	}
	defer func() { _ = cmdClient.Process.Kill() }()

	socksAddr := fmt.Sprintf("127.0.0.1:%d", socksPort)
	if err := waitListen(socksAddr, 15*time.Second); err != nil {
		serverBytes, _ := os.ReadFile(serverLogPath)
		clientBytes, _ := os.ReadFile(clientLogPath)
		t.Fatalf("Mieru %s client did not listen: %v\nserver log:\n%s\nclient log:\n%s\nclient config:\n%s", transport, err, serverBytes, clientBytes, modifiedClientJSON)
	}
	assertHTTPThroughSOCKS(t, socksAddr, backend.URL, expectedResponse)
}

func TestRequiredNaiveProxyDataPath(t *testing.T) {
	caddyPath := requiredBinary(t, "caddy")
	naivePath := requiredBinary(t, "naive")

	expectedResponse := "hello from naiveproxy"
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(expectedResponse))
	}))
	defer backend.Close()

	srv := startServer(t, serverOptions{token: "e2e-secret-token"})
	inboundPort := freePort(t)
	resp := srv.do(http.MethodPut, "/api/settings", `{"panelListen":"127.0.0.1:2096","mode":"dev","domain":"vpn.example.com","email":"test@example.com"}`)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("settings expected 200, got %d: %v", resp.StatusCode, readJSON(t, resp))
	}
	drain(resp)
	resp = srv.do(http.MethodPost, "/api/inbounds", fmt.Sprintf(`{"name":"naive-tcp","protocol":"naiveproxy","transport":"tcp","port":%d,"enabled":true,"naiveUsername":"naive-user","naivePassword":"naive-pass"}`, inboundPort))
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("inbound expected 201, got %d: %v", resp.StatusCode, readJSON(t, resp))
	}
	drain(resp)
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

	generatedPath := filepath.Join(srv.applyRoot, "generated", "caddy", "naive-tcp.Caddyfile")
	generated, err := os.ReadFile(generatedPath)
	if err != nil {
		t.Fatal(err)
	}
	caddyfile := strings.Replace(string(generated), fmt.Sprintf(":%d, vpn.example.com", inboundPort), fmt.Sprintf("127.0.0.1:%d", inboundPort), 1)
	// The data-path test runs on loopback without public DNS. Remove only the
	// generated TLS block; renderer tests separately require public ACME and
	// reject the production-incompatible internal issuer.
	tlsBlock := regexp.MustCompile(`(?ms)^\s*tls\s*\{.*?^\s*\}\s*$`)
	caddyfile = tlsBlock.ReplaceAllString(caddyfile, "")

	tempDir := t.TempDir()
	caddyfilePath := filepath.Join(tempDir, "Caddyfile")
	if err := os.WriteFile(caddyfilePath, []byte(caddyfile), 0o600); err != nil {
		t.Fatal(err)
	}
	serverLogPath := filepath.Join(tempDir, "caddy.log")
	serverLog, _ := os.Create(serverLogPath)
	defer serverLog.Close()
	cmdServer := exec.Command(caddyPath, "run", "--config", caddyfilePath, "--adapter", "caddyfile")
	cmdServer.Stdout = serverLog
	cmdServer.Stderr = serverLog
	if err := cmdServer.Start(); err != nil {
		t.Fatalf("start caddy: %v", err)
	}
	defer func() { _ = cmdServer.Process.Kill() }()
	serverAddr := fmt.Sprintf("127.0.0.1:%d", inboundPort)
	if err := waitListen(serverAddr, 15*time.Second); err != nil {
		logBytes, _ := os.ReadFile(serverLogPath)
		t.Fatalf("Caddy did not listen: %v\nCaddyfile:\n%s\nlog:\n%s", err, caddyfile, logBytes)
	}

	resp = srv.do(http.MethodGet, "/api/client-links", "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("client links expected 200, got %d", resp.StatusCode)
	}
	linksBody := readJSON(t, resp)
	linksRaw, _ := json.Marshal(linksBody["links"])
	var links []struct {
		Protocol string `json:"protocol"`
		URI      string `json:"uri"`
	}
	if err := json.Unmarshal(linksRaw, &links); err != nil {
		t.Fatal(err)
	}
	var accessURI string
	for _, link := range links {
		if link.Protocol == "naiveproxy" {
			accessURI = link.URI
			break
		}
	}
	if accessURI == "" {
		t.Fatal("panel did not return a NaiveProxy access URI")
	}
	parsed, err := url.Parse(accessURI)
	if err != nil {
		t.Fatal(err)
	}
	username := parsed.User.Username()
	password, _ := parsed.User.Password()
	socksPort := freePort(t)
	clientConfig := map[string]any{
		"listen":  fmt.Sprintf("socks://127.0.0.1:%d", socksPort),
		"proxy":   fmt.Sprintf("http://%s:%s@127.0.0.1:%d", url.QueryEscape(username), url.QueryEscape(password), inboundPort),
		"padding": true,
	}
	clientJSON, _ := json.Marshal(clientConfig)
	clientPath := filepath.Join(tempDir, "naive.json")
	if err := os.WriteFile(clientPath, clientJSON, 0o600); err != nil {
		t.Fatal(err)
	}
	clientLogPath := filepath.Join(tempDir, "naive.log")
	clientLog, _ := os.Create(clientLogPath)
	defer clientLog.Close()
	cmdClient := exec.Command(naivePath, clientPath)
	cmdClient.Stdout = clientLog
	cmdClient.Stderr = clientLog
	if err := cmdClient.Start(); err != nil {
		t.Fatalf("start naive: %v", err)
	}
	defer func() { _ = cmdClient.Process.Kill() }()
	socksAddr := fmt.Sprintf("127.0.0.1:%d", socksPort)
	if err := waitListen(socksAddr, 15*time.Second); err != nil {
		clientBytes, _ := os.ReadFile(clientLogPath)
		t.Fatalf("Naive client did not listen: %v\nclient log:\n%s\nconfig:\n%s", err, clientBytes, clientJSON)
	}
	assertHTTPThroughSOCKS(t, socksAddr, backend.URL, expectedResponse)
}

func assertHTTPThroughSOCKS(t *testing.T, socksAddr, targetURL, expected string) {
	t.Helper()
	dialer, err := proxy.SOCKS5("tcp", socksAddr, nil, proxy.Direct)
	if err != nil {
		t.Fatal(err)
	}
	client := &http.Client{
		Transport: &http.Transport{DialContext: func(_ context.Context, network, addr string) (net.Conn, error) {
			return dialer.Dial(network, addr)
		}},
		Timeout: 10 * time.Second,
	}
	resp, err := client.Get(targetURL)
	if err != nil {
		t.Fatalf("GET through SOCKS failed: %v", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != expected {
		t.Fatalf("response = %q, want %q", body, expected)
	}
}
