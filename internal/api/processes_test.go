package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	veilruntime "github.com/mikkelchokolate/Veil/internal/runtime"
)

// managedProcessFixtureEnv marks the child process spawned by
// startManagedProcessFixture. The child re-execs this test binary under the
// managed name "veil" so the /api/processes policy (managed names only) has a
// real entry — without it the endpoint's collection is empty in the test
// environment and per-field assertions could never execute (issue #1012).
const managedProcessFixtureEnv = "VEIL_MANAGED_PROCESS_FIXTURE"

// TestManagedProcessFixtureHelper is the child entrypoint spawned by
// startManagedProcessFixture. In the parent suite it must skip — that skip is
// helper-boundary evidence allowlisted in test-helper-allowlist.txt, not a
// standalone assertion.
func TestManagedProcessFixtureHelper(t *testing.T) {
	if os.Getenv(managedProcessFixtureEnv) != "1" {
		t.Skip("subprocess fixture")
	}
	// Sleep until the parent's cleanup kills us; long enough that a stalled
	// poll deadline never resurrects flakiness, short enough to leak-bounded
	// if the parent dies first.
	time.Sleep(5 * time.Minute)
}

// startManagedProcessFixture re-execs the test binary as "veil" — the managed
// name the runtime policy recognizes — so /api/processes discovery sees a real
// entry. Discovery matches on /proc/<pid>/comm (the on-disk basename), so the
// binary is hardlinked under the fixture name, falling back to a copy on
// filesystems that cannot link.
func startManagedProcessFixture(t *testing.T) {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatalf("resolve test binary: %v", err)
	}
	dir := t.TempDir()
	veilPath := filepath.Join(dir, "veil")
	if err := os.Link(exe, veilPath); err != nil {
		body, readErr := os.ReadFile(exe)
		if readErr != nil {
			t.Fatalf("read test binary for fixture copy: %v", readErr)
		}
		if writeErr := os.WriteFile(veilPath, body, 0o755); writeErr != nil {
			t.Fatalf("copy test binary for fixture: %v", writeErr)
		}
	}
	cmd := exec.Command(veilPath, "-test.run=^TestManagedProcessFixtureHelper$", "-test.count=1")
	cmd.Env = append(os.Environ(), managedProcessFixtureEnv+"=1")
	if err := cmd.Start(); err != nil {
		t.Fatalf("start managed-process fixture: %v", err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	})
}

// getProcesses issues GET /api/processes until a managed entry is reported
// (or the deadline passes), asserting the 200 status, a clean decode, and a
// real processes array on every poll.
func getProcesses(t *testing.T, r http.Handler) []interface{} {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		req := httptest.NewRequest(http.MethodGet, "/api/processes", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
		}
		var raw map[string]interface{}
		if err := json.NewDecoder(w.Body).Decode(&raw); err != nil {
			t.Fatalf("decode /api/processes: %v (body: %s)", err, w.Body.String())
		}
		// A null/absent processes key is the legitimate empty collection while
		// the fixture child is still exec'ing — keep polling. A non-null value
		// that is not an array is a malformed response: fail immediately.
		procs, ok := raw["processes"].([]interface{})
		if !ok && raw["processes"] != nil {
			t.Fatalf("expected processes array, got %T: %v", raw["processes"], raw["processes"])
		}
		if len(procs) > 0 {
			return procs
		}
		if time.Now().After(deadline) {
			t.Fatalf("managed-process fixture never appeared in /api/processes: %v", raw)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func TestProcessesEndpointRejectsNonGet(t *testing.T) {
	r, _ := newTestRouter(ServerInfo{Version: "test"})
	req := httptest.NewRequest(http.MethodPost, "/api/processes", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405, got %d", w.Code)
	}
}

func TestProcessesEndpointReturnsJSON(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("skipping on Windows")
	}
	r, _ := newTestRouter(ServerInfo{Version: "test"})
	req := httptest.NewRequest(http.MethodGet, "/api/processes", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if ct := w.Header().Get("Content-Type"); !strings.Contains(ct, "application/json") {
		t.Errorf("expected json content-type, got %q", ct)
	}
}

func TestProcessesEndpointFieldsPresent(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("skipping on Windows")
	}
	startManagedProcessFixture(t)
	r, _ := newTestRouter(ServerInfo{Version: "test"})
	procs := getProcesses(t, r)
	for _, entry := range procs {
		proc, ok := entry.(map[string]interface{})
		if !ok {
			t.Fatalf("process entry is %T, want object: %v", entry, entry)
		}
		for _, field := range []string{"pid", "name", "cpuPercent", "memoryMB", "uptimeSeconds"} {
			if _, ok := proc[field]; !ok {
				t.Errorf("missing field %s in %v", field, proc)
			}
		}
	}
}

func TestProcessesEndpointValuesValid(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("skipping on Windows")
	}
	startManagedProcessFixture(t)
	r, _ := newTestRouter(ServerInfo{Version: "test"})

	var stats veilruntime.ProcessesStats
	deadline := time.Now().Add(10 * time.Second)
	for {
		req := httptest.NewRequest(http.MethodGet, "/api/processes", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
		}
		stats = veilruntime.ProcessesStats{}
		if err := json.NewDecoder(w.Body).Decode(&stats); err != nil {
			t.Fatalf("decode /api/processes: %v (body: %s)", err, w.Body.String())
		}
		if len(stats.Processes) > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("managed-process fixture never appeared in /api/processes")
		}
		time.Sleep(50 * time.Millisecond)
	}
	for _, p := range stats.Processes {
		if p.PID <= 0 {
			t.Errorf("invalid PID %d for %s", p.PID, p.Name)
		}
		if p.Name == "" {
			t.Error("empty process name")
		}
		if p.MemoryMB < 0 {
			t.Errorf("negative memory for %s: %d", p.Name, p.MemoryMB)
		}
	}
}
