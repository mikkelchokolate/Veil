//go:build e2e

// Package e2e contains end-to-end tests that exercise a real `veil` binary:
// it is compiled once, launched as a subprocess bound to a real OS socket,
// driven over HTTP, and shut down gracefully. These tests are guarded by the
// `e2e` build tag so they do not run as part of the default `go test ./...`.
//
// Run with: go test -tags e2e ./test/e2e/...
package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mikkelchokolate/Veil/internal/privileged"
	"github.com/mikkelchokolate/Veil/internal/statecommit"
)

// buildOnce compiles the veil binary a single time per test process.
var (
	buildOnce   sync.Once
	builtBinary string
	buildErr    error
)

// veilBinary builds (once) and returns the path to a freshly compiled veil
// binary for the current module.
func veilBinary(t *testing.T) string {
	t.Helper()
	buildOnce.Do(func() {
		dir, err := os.MkdirTemp("", "veil-e2e-bin-")
		if err != nil {
			buildErr = err
			return
		}
		bin := filepath.Join(dir, "veil")
		if runtime.GOOS == "windows" {
			bin += ".exe"
		}
		// The e2e package lives at <module>/test/e2e, so the module root is two
		// directories up. Build the CLI entrypoint from there.
		cmd := exec.Command("go", "build", "-o", bin, "./cmd/veil")
		cmd.Dir = moduleRoot(dir)
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		if err := cmd.Run(); err != nil {
			buildErr = fmt.Errorf("build veil: %v: %s", err, stderr.String())
			return
		}
		builtBinary = bin
	})
	if buildErr != nil {
		t.Fatalf("build veil binary: %v", buildErr)
	}
	return builtBinary
}

// moduleRoot resolves the module root relative to this test file's working
// directory. `go test` runs with the working directory set to the package
// directory (test/e2e), so the module root is two levels up.
func moduleRoot(_ string) string {
	wd, err := os.Getwd()
	if err != nil {
		return "../.."
	}
	return filepath.Clean(filepath.Join(wd, "..", ".."))
}

// freePort asks the kernel for an unused TCP port and returns it. There is an
// inherent race between closing the listener and the server re-binding, but in
// practice it is reliable for local test orchestration.
func freePort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve free port: %v", err)
	}
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port
}

// serverProc is a running veil serve subprocess under test.
type serverProc struct {
	t         *testing.T
	cmd       *exec.Cmd
	baseURL   string
	token     string
	statePath string
	applyRoot string
	logBuf    *syncBuffer
	waitErr   chan error
	stdin     io.WriteCloser
}

// serverOptions configures a serve subprocess.
type serverOptions struct {
	// token is the API bearer token. Empty means auth disabled.
	token string
	// seedState, when non-empty, is written to the state file before launch.
	seedState string
	// extraEnv allows appending additional environment variables
	extraEnv []string
}

// startServer launches `veil serve` on a free port with a private temp state
// directory and waits until it is accepting connections.
func startServer(t *testing.T, opts serverOptions) *serverProc {
	t.Helper()
	bin := veilBinary(t)
	dir := t.TempDir()
	statePath := filepath.Join(dir, "state.json")
	applyRoot := filepath.Join(dir, "apply")
	keyPath := filepath.Join(dir, "state.key")
	if opts.seedState != "" {
		if err := os.WriteFile(statePath, []byte(opts.seedState), 0o600); err != nil {
			t.Fatalf("seed state: %v", err)
		}
	}
	port := freePort(t)
	addr := fmt.Sprintf("127.0.0.1:%d", port)
	helperSocket := startRecoveryHelper(t, dir, statePath)

	cmd := exec.Command(bin, "serve")
	cmd.Env = append(os.Environ(),
		"VEIL_LISTEN="+addr,
		"VEIL_STATE_PATH="+statePath,
		"VEIL_APPLY_ROOT="+applyRoot,
		"VEIL_KEY_PATH="+keyPath,
		"VEIL_HELPER_SOCKET="+helperSocket,
		"VEIL_SHUTDOWN_ON_STDIN_CLOSE=1",
	)
	if opts.token != "" {
		cmd.Env = append(cmd.Env, "VEIL_API_TOKEN="+opts.token)
	}
	if len(opts.extraEnv) > 0 {
		cmd.Env = append(cmd.Env, opts.extraEnv...)
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatalf("stdin pipe: %v", err)
	}
	logBuf := &syncBuffer{}
	cmd.Stdout = logBuf
	cmd.Stderr = logBuf
	if err := cmd.Start(); err != nil {
		t.Fatalf("start veil serve: %v", err)
	}

	p := &serverProc{
		t:         t,
		cmd:       cmd,
		baseURL:   "http://" + addr,
		token:     opts.token,
		statePath: statePath,
		applyRoot: applyRoot,
		logBuf:    logBuf,
		waitErr:   make(chan error, 1),
		stdin:     stdin,
	}
	go func() { p.waitErr <- cmd.Wait() }()
	t.Cleanup(p.stop)
	p.waitUntilListening()
	return p
}

func startRecoveryHelper(t *testing.T, dir, statePath string) string {
	t.Helper()
	socketPath := filepath.Join(dir, "helper.sock")
	applyRoot := filepath.Join(dir, "apply")
	policy := privileged.DefaultPolicy()
	policy.StagingRoot = filepath.Join(applyRoot, "generated")
	policy.GeneratedRoot = filepath.Join(applyRoot, "live")
	policy.StateRoot = dir
	policy.StatePath = statePath
	policy.KeyPath = filepath.Join(dir, "state.key")
	policy.BackupPassphrasePath = filepath.Join(dir, "backup.passphrase")
	policy.BackupRoot = filepath.Join(dir, "backups")
	policy.UpdateRoot = filepath.Join(dir, "updates")
	policy.FencePath = filepath.Join(dir, "runtime-fence.json")
	promotionExecutor := privileged.NewProductionExecutor(privileged.ProductionConfig{
		PromotionBackupRoot: filepath.Join(dir, "promotion-backups"),
		StatePath:           statePath,
		KeyPath:             policy.KeyPath,
	})
	server := privileged.NewServer(privileged.NewLocalAdapter(policy, privileged.Executor{
		Promote: promotionExecutor.Promote,
		ServiceAction: func(context.Context, privileged.ServiceActionRequest) error {
			return nil
		},
		ServiceStatus: func(_ context.Context, request privileged.ServiceStatusRequest) (privileged.ServiceStatusResult, error) {
			result := privileged.ServiceStatusResult{Services: make([]privileged.ServiceStatus, 0, len(request.Units))}
			for _, unit := range request.Units {
				result.Services = append(result.Services, privileged.ServiceStatus{
					Unit: unit, LoadState: "loaded", ActiveState: "active", SubState: "running",
				})
			}
			return result, nil
		},
		Firewall: func(_ context.Context, request privileged.ResolvedFirewall) (privileged.FirewallResult, error) {
			result := privileged.FirewallResult{TransactionID: request.TransactionID, AppliedRuleIDs: append([]string(nil), request.RuleIDs...)}
			if request.Action == privileged.FirewallActionPrepare {
				result.TransactionID = request.TransactionID
				if result.TransactionID == "" {
					result.TransactionID = "e2e-firewall-transaction"
				}
				result.Prepared = true
			}
			return result, nil
		},
		CaddyLoad: func(context.Context, privileged.CaddyLoadRequest) error { return nil },
		RecoverKeyRotation: func(context.Context) error {
			return statecommit.RecoverKeyRotation(statecommit.RecoverKeyRotationOptions{StatePath: statePath})
		},
	}))
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- server.ServeUnix(ctx, socketPath, uint32(os.Getuid()), true)
	}()
	t.Cleanup(func() {
		cancel()
		select {
		case err := <-done:
			if err != nil {
				t.Errorf("stop recovery helper: %v", err)
			}
		case <-time.After(5 * time.Second):
			t.Errorf("recovery helper did not stop")
		}
	})
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case err := <-done:
			t.Fatalf("recovery helper exited before listening: %v", err)
		default:
		}
		if info, err := os.Stat(socketPath); err == nil && info.Mode()&os.ModeSocket != 0 {
			return socketPath
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("recovery helper did not listen at %s", socketPath)
	return ""
}

// waitUntilListening blocks until the server accepts a TCP connection or the
// deadline elapses.
func (p *serverProc) waitUntilListening() {
	p.t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case err := <-p.waitErr:
			p.t.Fatalf("server exited before listening: %v\nlogs:\n%s", err, p.logBuf.String())
		default:
		}
		conn, err := net.DialTimeout("tcp", strings.TrimPrefix(p.baseURL, "http://"), 250*time.Millisecond)
		if err == nil {
			conn.Close()
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	p.t.Fatalf("server did not start listening within deadline\nlogs:\n%s", p.logBuf.String())
}

// do issues an HTTP request against the server, attaching the bearer token
// when one is configured.
func (p *serverProc) do(method, path, body string) *http.Response {
	p.t.Helper()
	var rdr io.Reader
	if body != "" {
		rdr = strings.NewReader(body)
	}
	req, err := http.NewRequest(method, p.baseURL+path, rdr)
	if err != nil {
		p.t.Fatalf("build request %s %s: %v", method, path, err)
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if p.token != "" {
		req.Header.Set("Authorization", "Bearer "+p.token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		p.t.Fatalf("request %s %s: %v", method, path, err)
	}
	return resp
}

// doNoAuth issues a request without the bearer token, for auth-gating checks.
func (p *serverProc) doNoAuth(method, path string) *http.Response {
	p.t.Helper()
	req, err := http.NewRequest(method, p.baseURL+path, nil)
	if err != nil {
		p.t.Fatalf("build request %s %s: %v", method, path, err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		p.t.Fatalf("request %s %s: %v", method, path, err)
	}
	return resp
}

// readJSON decodes a response body into a generic map and closes the body.
func readJSON(t *testing.T, resp *http.Response) map[string]any {
	t.Helper()
	defer resp.Body.Close()
	var out map[string]any
	data, _ := io.ReadAll(resp.Body)
	if len(data) == 0 {
		return out
	}
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("decode JSON: %v (body: %s)", err, string(data))
	}
	return out
}

// drain reads and closes a response body.
func drain(resp *http.Response) {
	if resp == nil {
		return
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()
}

// stop sends SIGINT and asserts the process exits cleanly within the drain
// window. Safe to call multiple times.
func (p *serverProc) stop() {
	if p.cmd.Process == nil {
		return
	}
	if p.stdin != nil {
		_ = p.stdin.Close()
	}
	select {
	case err := <-p.waitErr:
		if err != nil {
			p.t.Errorf("server did not shut down cleanly: %v\nlogs:\n%s", err, p.logBuf.String())
		}
	case <-time.After(10 * time.Second):
		_ = p.cmd.Process.Kill()
		p.t.Errorf("server did not exit after SIGINT within deadline\nlogs:\n%s", p.logBuf.String())
	}
	p.cmd.Process = nil
}

// gracefulShutdown signals the server and verifies it exits with code 0 and
// logs a clean stop. Returns the captured logs.
func (p *serverProc) gracefulShutdown() string {
	p.t.Helper()
	if p.stdin != nil {
		_ = p.stdin.Close()
	}
	select {
	case err := <-p.waitErr:
		if err != nil {
			p.t.Fatalf("graceful shutdown failed (expected exit 0): %v\nlogs:\n%s", err, p.logBuf.String())
		}
	case <-time.After(10 * time.Second):
		_ = p.cmd.Process.Kill()
		p.t.Fatalf("server did not exit after SIGINT\nlogs:\n%s", p.logBuf.String())
	}
	p.cmd.Process = nil
	return p.logBuf.String()
}

// runCLI runs the veil binary with the given args and returns combined output.
func runCLI(t *testing.T, env []string, args ...string) (string, error) {
	t.Helper()
	bin := veilBinary(t)
	cmd := exec.CommandContext(context.Background(), bin, args...)
	cmd.Env = append(os.Environ(), env...)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// syncBuffer is a goroutine-safe buffer for capturing subprocess output.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}
