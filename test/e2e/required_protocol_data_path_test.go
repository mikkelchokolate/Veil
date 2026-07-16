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

	const backendHost = "veil-protocol-e2e.test"
	if err := exec.Command("sudo", "sh", "-c", "printf '127.0.0.1 "+backendHost+"\\n' >> /etc/hosts").Run(); err != nil {
		t.Fatalf("register local E2E hostname: %v", err)
	}
	backendURL := strings.Replace(backend.URL, "127.0.0.1", backendHost, 1)

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
	applyPanelConfiguration(t, srv)

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
	defer func() { _ = cmdServer.Process.Kill() }()

	if err := waitUnixSocket(serverSocket, 15*time.Second); err != nil {
		logBytes, _ := os.ReadFile(serverLogPath)
		t.Fatalf("mita control socket did not appear: %v\nlog:\n%s", err, logBytes)
	}
	runMieruControl(t, serverEnv, mitaPath, serverLogPath, "apply", "config", serverConfig)
	runMieruControl(t, serverEnv, mitaPath, serverLogPath, "start")

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
		t.Fatal(err)
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
		t.Fatal(err)
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
	clientRuntime := t.TempDir()
	clientFile := filepath.Join(clientRuntime, "client.json")
	if err := os.WriteFile(clientFile, modifiedClientJSON, 0o600); err != nil {
		t.Fatal(err)
	}
	clientLogPath := filepath.Join(clientRuntime, "mieru.log")
	clientLog, err := os.Create(clientLogPath)
	if err != nil {
		t.Fatal(err)
	}
	defer clientLog.Close()

	cmdClient := exec.Command(mieruPath, "run")
	cmdClient.Env = append(os.Environ(),
		"MIERU_CONFIG_JSON_FILE="+clientFile,
		"MIERU_LOG_NO_TIMESTAMP=true",
	)
	cmdClient.Stdout = clientLog
	cmdClient.Stderr = clientLog
	if err := cmdClient.Start(); err != nil {
		t.Fatalf("start mieru client: %v", err)
	}
	defer func() { _ = cmdClient.Process.Kill() }()

	socksAddr := fmt.Sprintf("127.0.0.1:%d", socksPort)
	if err := waitListen(socksAddr, 15*time.Second); err != nil {
		serverBytes, _ := os.ReadFile(serverLogPath)
		clientBytes, _ := os.ReadFile(clientLogPath)
		t.Fatalf("Mieru %s client did not listen: %v\nserver log:\n%s\nclient log:\n%s\nclient config:\n%s", transport, err, serverBytes, clientBytes, modifiedClientJSON)
	}
	assertHTTPThroughSOCKS(t, socksAddr, backendURL, expectedResponse)
}

func applyPanelConfiguration(t *testing.T, srv *testServer) {
	t.Helper()
	resp := srv.do(http.MethodPost, "/api/apply/plan", "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("apply plan expected 200, got %d: %v", resp.StatusCode, readJSON(t, resp))
	}
	drain(resp)

	resp = srv.do(http.MethodPost, "/api/apply", `{"confirm":true}`)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("apply expected 200, got %d: %v", resp.StatusCode, readJSON(t, resp))
	}
	drain(resp)
}

func runMieruControl(t *testing.T, env []string, binary, logPath string, args ...string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, binary, args...)
	cmd.Env = env
	output, err := cmd.CombinedOutput()
	if err != nil {
		logBytes, _ := os.ReadFile(logPath)
		t.Fatalf("%s %s failed: %v\noutput:\n%s\ndaemon log:\n%s", binary, strings.Join(args, " "), err, output, logBytes)
	}
}

func waitUnixSocket(path string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		info, err := os.Stat(path)
		if err == nil && info.Mode()&os.ModeSocket != 0 {
			return nil
		}
		time.Sleep(50 * time.Millisecond)
	}
	return fmt.Errorf("Unix socket %s did not appear within %v", path, timeout)
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
	applyPanelConfiguration(t, srv)

	generatedPath := filepath.Join(srv.applyRoot, "generated", "caddy", "naive-tcp.Caddyfile")
	generated, err := os.ReadFile(generatedPath)
	if err != nil {
		t.Fatal(err)
	}

	tempDir := t.TempDir()
	certPath := filepath.Join(tempDir, "localhost.crt")
	keyPath := filepath.Join(tempDir, "localhost.key")
	if err := generateSelfSignedCert(certPath, keyPath); err != nil {
		t.Fatalf("generate trusted test certificate: %v", err)
	}
	caPath := "/usr/local/share/ca-certificates/veil-naive-e2e.crt"
	if output, err := exec.Command("sudo", "install", "-m", "0644", certPath, caPath).CombinedOutput(); err != nil {
		t.Fatalf("install test certificate: %v: %s", err, output)
	}
	defer func() {
		_ = exec.Command("sudo", "rm", "-f", caPath).Run()
		_ = exec.Command("sudo", "update-ca-certificates").Run()
	}()
	if output, err := exec.Command("sudo", "update-ca-certificates").CombinedOutput(); err != nil {
		t.Fatalf("refresh test trust store: %v: %s", err, output)
	}

	caddyfile := strings.Replace(string(generated), fmt.Sprintf(":%d, vpn.example.com", inboundPort), fmt.Sprintf("https://localhost:%d", inboundPort), 1)
	caddyfile, err = removeCaddyDirectiveBlock(caddyfile, "tls")
	if err != nil {
		t.Fatal(err)
	}
	caddyfile = strings.Replace(caddyfile, "  encode", fmt.Sprintf("  tls %s %s\n  encode", certPath, keyPath), 1)

	caddyfilePath := filepath.Join(tempDir, "Caddyfile")
	if err := os.WriteFile(caddyfilePath, []byte(caddyfile), 0o600); err != nil {
		t.Fatal(err)
	}
	serverLogPath := filepath.Join(tempDir, "caddy.log")
	serverLog, err := os.Create(serverLogPath)
	if err != nil {
		t.Fatal(err)
	}
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
		"proxy":   fmt.Sprintf("https://%s:%s@localhost:%d", url.QueryEscape(username), url.QueryEscape(password), inboundPort),
		"padding": true,
		"log":     "",
	}
	clientJSON, err := json.Marshal(clientConfig)
	if err != nil {
		t.Fatal(err)
	}
	clientPath := filepath.Join(tempDir, "naive.json")
	if err := os.WriteFile(clientPath, clientJSON, 0o600); err != nil {
		t.Fatal(err)
	}
	clientLogPath := filepath.Join(tempDir, "naive.log")
	clientLog, err := os.Create(clientLogPath)
	if err != nil {
		t.Fatal(err)
	}
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
	if err := assertHTTPThroughSOCKSResult(socksAddr, backend.URL, expectedResponse); err != nil {
		serverBytes, _ := os.ReadFile(serverLogPath)
		clientBytes, _ := os.ReadFile(clientLogPath)
		t.Fatalf("Naive HTTPS data path failed: %v\nCaddyfile:\n%s\nserver log:\n%s\nclient log:\n%s\nclient config:\n%s", err, caddyfile, serverBytes, clientBytes, clientJSON)
	}
}

func removeCaddyDirectiveBlock(input, directive string) (string, error) {
	lines := strings.Split(input, "\n")
	start := -1
	depth := 0
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if start == -1 {
			if strings.HasPrefix(trimmed, directive+" ") || trimmed == directive+"{" || trimmed == directive+" {" {
				if strings.Contains(trimmed, "{") {
					start = i
					depth = strings.Count(line, "{") - strings.Count(line, "}")
				}
			}
			continue
		}
		depth += strings.Count(line, "{") - strings.Count(line, "}")
		if depth == 0 {
			return strings.Join(append(lines[:start], lines[i+1:]...), "\n"), nil
		}
	}
	return "", fmt.Errorf("Caddy directive block %q not found or unbalanced", directive)
}

func assertHTTPThroughSOCKS(t *testing.T, socksAddr, targetURL, expected string) {
	t.Helper()
	if err := assertHTTPThroughSOCKSResult(socksAddr, targetURL, expected); err != nil {
		t.Fatal(err)
	}
}

func assertHTTPThroughSOCKSResult(socksAddr, targetURL, expected string) error {
	dialer, err := proxy.SOCKS5("tcp", socksAddr, nil, proxy.Direct)
	if err != nil {
		return err
	}
	client := &http.Client{
		Transport: &http.Transport{
			DialContext: func(_ context.Context, network, addr string) (net.Conn, error) {
				return dialer.Dial(network, addr)
			},
		},
		Timeout: 10 * time.Second,
	}
	resp, err := client.Get(targetURL)
	if err != nil {
		return fmt.Errorf("GET through SOCKS failed: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if string(body) != expected {
		return fmt.Errorf("response = %q, want %q", body, expected)
	}
	return nil
}
