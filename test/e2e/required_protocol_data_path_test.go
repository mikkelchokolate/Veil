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

func TestRequiredHysteria2DataPath(t *testing.T) {
	testHysteria2DataPath(t, requiredBinary(t, "hysteria"))
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

	backendHost := "veil-mieru-" + transport + "-e2e.test"
	registerLoopbackHostname(t, backendHost)
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
	// Regression guard (audit series #51/#101): the delivered client config
	// must be importable as-is — upstream mieru rejects socks5Port < 1 with
	// "socks5 port number 0 is invalid". The renderer now emits a valid
	// deterministic port, so assert it before the runtime override below.
	socksPortValue, ok := clientMap["socks5Port"].(float64)
	if !ok || int(socksPortValue) < 1 {
		t.Fatalf("delivered Mieru client config has invalid socks5Port: %v (config must be importable without patching)", clientMap["socks5Port"])
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
		applyResponse := readJSON(t, resp)
		stateResponse := readJSON(t, srv.do(http.MethodGet, "/api/apply/state", ""))
		jobsResponse := readJSON(t, srv.do(http.MethodGet, "/api/apply/jobs", ""))
		t.Fatalf("apply expected 200, got %d: %v\napply state: %v\napply jobs: %v\nserver log:\n%s", resp.StatusCode, applyResponse, stateResponse, jobsResponse, srv.logBuf.String())
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
	testNaiveProxyDataPath(t, caddyPath, naivePath)
}

func testNaiveProxyDataPath(t *testing.T, caddyPath, naivePath string) {
	t.Helper()
	expectedResponse := "hello from naiveproxy"
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(expectedResponse))
	}))
	defer backend.Close()
	const backendHost = "veil-naive-e2e.test"
	registerLoopbackHostname(t, backendHost)
	backendURL := strings.Replace(backend.URL, "127.0.0.1", backendHost, 1)

	srv := startServer(t, serverOptions{token: "e2e-secret-token"})
	inboundPort := freePort(t)
	settingsBody := fmt.Sprintf(`{"panelListen":"127.0.0.1:2096","panelAccess":"caddy","webBasePath":"/panel-e2e/","panelDomain":"vpn.example.com","panelEmail":"test@example.com","panelPublicPort":%d,"mode":"dev","domain":"vpn.example.com","email":"test@example.com","defaultAcmeEmail":"test@example.com"}`, inboundPort)
	resp := srv.do(http.MethodPut, "/api/settings", settingsBody)
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

	generatedPath := filepath.Join(srv.applyRoot, "generated", "caddy", "config.json")
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
	// The standalone naive binary (C++) has no flag to skip TLS verification or
	// point at a custom CA bundle, so the only way to make it trust our
	// self-signed server cert is the system trust store. Scope the damage: use a
	// unique per-run CA filename, register removal via t.Cleanup (runs even on
	// Fatal/panic), refresh the store on cleanup, and assert the file is gone.
	caName := "veil-naive-e2e-" + strings.NewReplacer("-", "", ".", "", "/", "").Replace(t.Name()) + ".crt"
	caPath := "/usr/local/share/ca-certificates/" + caName
	if output, err := exec.Command("sudo", "install", "-m", "0644", certPath, caPath).CombinedOutput(); err != nil {
		t.Fatalf("install test certificate: %v: %s", err, output)
	}
	t.Cleanup(func() {
		_ = exec.Command("sudo", "rm", "-f", caPath).Run()
		_ = exec.Command("sudo", "update-ca-certificates", "--fresh").Run()
		if _, err := os.Lstat(caPath); !os.IsNotExist(err) {
			t.Errorf("test CA %s was not removed from the system trust store", caPath)
		}
	})
	if output, err := exec.Command("sudo", "update-ca-certificates").CombinedOutput(); err != nil {
		t.Fatalf("refresh test trust store: %v: %s", err, output)
	}

	testConfig, err := configureNaiveCaddyJSONForLocalTLS(
		generated,
		fmt.Sprintf("127.0.0.1:%d", inboundPort),
		fmt.Sprintf("127.0.0.1:%d", freePort(t)),
		certPath,
		keyPath,
		backendHost,
	)
	if err != nil {
		t.Fatal(err)
	}
	caddyConfigPath := filepath.Join(tempDir, "caddy.json")
	if err := os.WriteFile(caddyConfigPath, testConfig, 0o600); err != nil {
		t.Fatal(err)
	}
	serverLogPath := filepath.Join(tempDir, "caddy.log")
	serverLog, err := os.Create(serverLogPath)
	if err != nil {
		t.Fatal(err)
	}
	defer serverLog.Close()

	cmdServer := exec.Command(caddyPath, "run", "--config", caddyConfigPath)
	cmdServer.Stdout = serverLog
	cmdServer.Stderr = serverLog
	if err := cmdServer.Start(); err != nil {
		t.Fatalf("start caddy: %v", err)
	}
	defer func() { _ = cmdServer.Process.Kill() }()
	serverAddr := fmt.Sprintf("127.0.0.1:%d", inboundPort)
	if err := waitListen(serverAddr, 15*time.Second); err != nil {
		logBytes, _ := os.ReadFile(serverLogPath)
		t.Fatalf("Caddy did not listen: %v\nCaddy JSON:\n%s\nlog:\n%s", err, testConfig, logBytes)
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
	if err := assertHTTPThroughSOCKSResult(socksAddr, backendURL, expectedResponse); err != nil {
		serverBytes, _ := os.ReadFile(serverLogPath)
		clientBytes, _ := os.ReadFile(clientLogPath)
		t.Fatalf("Naive HTTPS data path failed: %v\nCaddy JSON:\n%s\nserver log:\n%s\nclient log:\n%s\nclient config:\n%s", err, testConfig, serverBytes, clientBytes, clientJSON)
	}
}

func configureNaiveCaddyJSONForLocalTLS(input []byte, listenAddr, adminAddr, certPath, keyPath, allowedBackendHost string) ([]byte, error) {
	var cfg map[string]any
	if err := json.Unmarshal(input, &cfg); err != nil {
		return nil, fmt.Errorf("decode generated Caddy JSON: %w", err)
	}
	admin, ok := cfg["admin"].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("generated Caddy JSON has no admin object")
	}
	admin["listen"] = adminAddr
	cfg["logging"] = map[string]any{
		"logs": map[string]any{
			"default": map[string]any{"level": "DEBUG"},
		},
	}
	apps, ok := cfg["apps"].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("generated Caddy JSON has no apps object")
	}
	httpApp, ok := apps["http"].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("generated Caddy JSON has no http app")
	}
	servers, ok := httpApp["servers"].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("generated Caddy JSON has no HTTP servers")
	}
	var naiveServer map[string]any
	for _, rawServer := range servers {
		server, ok := rawServer.(map[string]any)
		if !ok || !caddyServerHasHandler(server, "forward_proxy") {
			continue
		}
		if naiveServer != nil {
			return nil, fmt.Errorf("generated Caddy JSON contains multiple NaiveProxy servers")
		}
		naiveServer = server
	}
	if naiveServer == nil {
		return nil, fmt.Errorf("generated Caddy JSON contains no NaiveProxy server")
	}
	if _, ok := naiveServer["tls_connection_policies"]; !ok {
		return nil, fmt.Errorf("generated NaiveProxy server does not explicitly enable TLS")
	}
	forwardProxy := caddyServerHandler(naiveServer, "forward_proxy")
	if forwardProxy == nil {
		return nil, fmt.Errorf("generated NaiveProxy server has no forward_proxy handler")
	}
	// The module's secure default ACL denies loopback networks. Allow only the
	// synthetic test hostname so the real proxy can reach the local HTTP backend
	// without weakening production renderer defaults.
	forwardProxy["acl"] = []map[string]any{{
		"subjects": []string{allowedBackendHost},
		"allow":    true,
	}}
	naiveServer["listen"] = []string{listenAddr}
	httpApp["servers"] = map[string]any{"naive-e2e": naiveServer}
	apps["tls"] = map[string]any{
		"certificates": map[string]any{
			"load_files": []map[string]any{{
				"certificate": certPath,
				"key":         keyPath,
			}},
		},
	}
	return json.MarshalIndent(cfg, "", "  ")
}

func caddyServerHasHandler(server map[string]any, handlerName string) bool {
	return caddyServerHandler(server, handlerName) != nil
}

func caddyServerHandler(server map[string]any, handlerName string) map[string]any {
	routes, _ := server["routes"].([]any)
	for _, rawRoute := range routes {
		route, _ := rawRoute.(map[string]any)
		handlers, _ := route["handle"].([]any)
		for _, rawHandler := range handlers {
			handler, _ := rawHandler.(map[string]any)
			if handler["handler"] == handlerName {
				return handler
			}
		}
	}
	return nil
}

func registerLoopbackHostname(t *testing.T, hostname string) {
	t.Helper()
	if hostname == "" || strings.ContainsAny(hostname, " 	\r\n") {
		t.Fatalf("invalid E2E hostname %q", hostname)
	}
	// Unique marker so cleanup removes only the exact line this invocation
	// appended, never a pre-existing entry that merely shares the hostname.
	marker := "veil-e2e-" + strings.NewReplacer("-", "", ".", "").Replace(t.Name()) + "-" + hostname
	cmd := exec.Command("sudo", "tee", "-a", "/etc/hosts")
	cmd.Stdin = strings.NewReader("127.0.0.1 " + hostname + " # " + marker + "\n")
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("register local E2E hostname: %v: %s", err, output)
	}
	t.Cleanup(func() {
		const cleanup = `
from pathlib import Path
import sys
p = Path("/etc/hosts")
marker = sys.argv[1]
lines = [line for line in p.read_text().splitlines() if marker not in line]
p.write_text("\n".join(lines) + "\n")
`
		if output, err := exec.Command("sudo", "python3", "-c", cleanup, marker).CombinedOutput(); err != nil {
			t.Errorf("remove local E2E hostname: %v: %s", err, output)
		}
	})
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
