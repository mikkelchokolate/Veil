package cli

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	serveflow "github.com/mikkelchokolate/Veil/internal/cliflow/serve"
	statusflow "github.com/mikkelchokolate/Veil/internal/cliflow/status"
)

func TestHealthcheckCommandUsesContainerContract(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/secret-panel/healthz" {
			http.NotFound(w, r)
			return
		}
		if r.Header.Get("X-Veil-Token") != "probe-token" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	path := filepath.Join(t.TempDir(), "container-health.json")
	contract := statusflow.ContractFromServe(server.Listener.Addr().String(), false, "/secret-panel/", "", "")
	if err := statusflow.WriteContract(path, contract); err != nil {
		t.Fatal(err)
	}
	t.Setenv("VEIL_CONTAINER_HEALTH_PATH", path)
	t.Setenv("VEIL_API_TOKEN", "probe-token")

	cmd := NewRootCommand("test")
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"healthcheck"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("healthcheck: %v\n%s", err, out.String())
	}
}

func TestHealthcheckCommandFailsWhenServerUnavailable(t *testing.T) {
	path := filepath.Join(t.TempDir(), "container-health.json")
	if err := statusflow.WriteContract(path, statusflow.ContractFromServe("127.0.0.1:1", false, "/", "", "")); err != nil {
		t.Fatal(err)
	}
	t.Setenv("VEIL_CONTAINER_HEALTH_PATH", path)

	cmd := NewRootCommand("test")
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"healthcheck"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected healthcheck failure")
	}
}

func TestWriteContainerHealthContractPersistsListenSchemeAndBasePath(t *testing.T) {
	path := filepath.Join(t.TempDir(), "container-health.json")
	t.Setenv("VEIL_CONTAINER_HEALTH_PATH", path)
	cfg := serveflow.Config{
		Listen:        "0.0.0.0:443",
		TLSEnabled:    true,
		WebBasePath:   "/secret/",
		TLSCert:       filepath.Join(t.TempDir(), "tls.crt"),
		AutoTLSDomain: "panel.example.com",
	}
	if err := writeContainerHealthContract(cfg); err != nil {
		t.Fatal(err)
	}
	got, err := statusflow.ReadContract(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.Listen != "0.0.0.0:443" || got.Scheme != "https" || got.WebBasePath != "/secret/" || got.ServerName != "panel.example.com" {
		t.Fatalf("contract=%+v", got)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var parsed map[string]any
	if err := json.Unmarshal(raw, &parsed); err != nil {
		t.Fatal(err)
	}
	if _, ok := parsed["authToken"]; ok {
		t.Fatalf("contract stored auth token: %s", raw)
	}
}

// TestContractProbePathFollowsVarDirRoot pins issue #753: the probe path must
// derive from the state root so custom-root containers health-check the file
// `veil serve` actually wrote — not a packaged /var/lib/veil literal.
func TestContractProbePathFollowsVarDirRoot(t *testing.T) {
	varDir := t.TempDir()
	t.Setenv("VEIL_CONTAINER_HEALTH_PATH", "")
	t.Setenv("VEIL_STATE_PATH", "")
	t.Setenv("VEIL_VAR_DIR", varDir)
	if got, want := statusflow.ContractProbePath(), filepath.Join(varDir, "container-health.json"); got != want {
		t.Fatalf("ContractProbePath() = %q, want %q", got, want)
	}
}

// TestContractProbePathHonoursExplicitOverride keeps the explicit leaf env
// authoritative over the derived root — same precedence as the other leaf
// paths in the entrypoint.
func TestContractProbePathHonoursExplicitOverride(t *testing.T) {
	explicit := filepath.Join(t.TempDir(), "custom-health.json")
	t.Setenv("VEIL_CONTAINER_HEALTH_PATH", explicit)
	t.Setenv("VEIL_VAR_DIR", t.TempDir())
	if got := statusflow.ContractProbePath(); got != explicit {
		t.Fatalf("ContractProbePath() = %q, want explicit %q", got, explicit)
	}
}

// TestHealthcheckReadsDerivedContract exercises the full probe path without an
// explicit VEIL_CONTAINER_HEALTH_PATH — the shape a HEALTHCHECK exec sees when
// the image runs with a custom VEIL_VAR_DIR.
func TestHealthcheckReadsDerivedContract(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	varDir := t.TempDir()
	path := filepath.Join(varDir, "container-health.json")
	if err := statusflow.WriteContract(path, statusflow.ContractFromServe(server.Listener.Addr().String(), false, "/", "", "")); err != nil {
		t.Fatal(err)
	}
	t.Setenv("VEIL_CONTAINER_HEALTH_PATH", "")
	t.Setenv("VEIL_STATE_PATH", "")
	t.Setenv("VEIL_VAR_DIR", varDir)

	cmd := NewRootCommand("test")
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"healthcheck"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("healthcheck against derived path: %v\n%s", err, out.String())
	}
}

func TestHealthcheckCommandIsHiddenFromRootHelp(t *testing.T) {
	help := executeHelp(t, "--help")
	if strings.Contains(help, "healthcheck") {
		t.Fatalf("operator help listed hidden healthcheck:\n%s", help)
	}
}
