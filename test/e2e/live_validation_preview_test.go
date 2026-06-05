//go:build e2e

package e2e

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestLiveValidationAndStructuredPreviewEndToEnd(t *testing.T) {
	srv := startServer(t, serverOptions{token: "validation-e2e-token"})
	response := srv.do(http.MethodPut, "/api/settings", `{"panelListen":"127.0.0.1:2096","panelAccess":"local","mode":"server"}`)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("settings expected 200, got %d: %v", response.StatusCode, readJSON(t, response))
	}
	drain(response)

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve busy port: %v", err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	candidate := fmt.Sprintf(
		`{"settings":{"panelListen":"127.0.0.1:2096","panelAccess":"local","mode":"server"},"inbounds":[{"name":"edge","protocol":"mieru","transport":"tcp","port":%d,"enabled":true,"password":"preview-secret"}],"warp":{"enabled":false}}`,
		port,
	)

	response = srv.do(http.MethodPost, "/api/validation", candidate)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("validation expected 200, got %d: %v", response.StatusCode, readJSON(t, response))
	}
	validation := readJSON(t, response)
	if valid, _ := validation["valid"].(bool); valid {
		t.Fatalf("busy candidate unexpectedly valid: %+v", validation)
	}
	if !jsonContainsIssue(validation, "port_in_use") {
		t.Fatalf("busy candidate missing port_in_use: %+v", validation)
	}

	inboundBody := fmt.Sprintf(
		`{"name":"edge","protocol":"mieru","transport":"tcp","port":%d,"enabled":true,"password":"preview-secret"}`,
		port,
	)
	response = srv.do(http.MethodPost, "/api/inbounds", inboundBody)
	if response.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("busy save expected 422, got %d: %s", response.StatusCode, readBody(response))
	}
	if body := readBody(srv.do(http.MethodGet, "/api/inbounds", "")); strings.TrimSpace(body) != "[]" {
		t.Fatalf("failed save mutated Inbounds: %s", body)
	}

	if err := listener.Close(); err != nil {
		t.Fatalf("release busy port: %v", err)
	}
	response = srv.do(http.MethodPost, "/api/inbounds", inboundBody)
	if response.StatusCode != http.StatusCreated {
		t.Fatalf("released save expected 201, got %d: %s", response.StatusCode, readBody(response))
	}
	drain(response)

	ownedListener, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
	if err != nil {
		t.Fatalf("occupy persisted binding: %v", err)
	}
	defer ownedListener.Close()
	response = srv.do(http.MethodPost, "/api/validation", candidate)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("owned validation expected 200, got %d: %v", response.StatusCode, readJSON(t, response))
	}
	owned := readJSON(t, response)
	if valid, _ := owned["valid"].(bool); !valid {
		t.Fatalf("unchanged persisted binding should be accepted: %+v", owned)
	}

	response = srv.do(http.MethodPost, "/api/apply/plan", "")
	if response.StatusCode != http.StatusOK {
		t.Fatalf("apply plan expected 200, got %d: %v", response.StatusCode, readJSON(t, response))
	}
	rawPlan := readBody(response)
	for _, want := range []string{`"operations"`, `"type":"promote_file"`, `"type":"restart_service"`, `"interruptionRisk":"connection-drop"`, `"rollbackAvailable":true`, `"validationSource":"managed-unit-catalog"`} {
		if !strings.Contains(rawPlan, want) {
			t.Fatalf("structured preview missing %s: %s", want, rawPlan)
		}
	}
	if strings.Contains(rawPlan, "preview-secret") {
		t.Fatalf("structured preview leaked credential: %s", rawPlan)
	}
}

func TestInvalidSeededStateCannotReachApplyStaging(t *testing.T) {
	seed := `{
	  "schemaVersion": 3,
	  "settings": {"panelListen":"127.0.0.1:2096","panelAccess":"local","mode":"server"},
	  "inbounds": [
	    {"name":"first","protocol":"mieru","transport":"tcp","port":24443,"enabled":true,"password":"first-secret"},
	    {"name":"second","protocol":"mieru","transport":"tcp","port":24443,"enabled":true,"password":"second-secret"}
	  ],
	  "routingRules": [],
	  "routingSource": {},
	  "warp": {"enabled":false},
	  "users": []
	}`
	srv := startServer(t, serverOptions{token: "invalid-apply-token", seedState: seed})

	response := srv.do(http.MethodPost, "/api/apply", `{"confirm":true}`)
	if response.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("invalid apply expected 422, got %d: %s", response.StatusCode, readBody(response))
	}
	if generatedFiles(t, filepath.Join(srv.applyRoot, "generated")) != 0 {
		t.Fatalf("invalid apply wrote staged files under %s", srv.applyRoot)
	}
}

func jsonContainsIssue(payload map[string]any, code string) bool {
	issues, _ := payload["issues"].([]any)
	for _, raw := range issues {
		issue, _ := raw.(map[string]any)
		if issue["code"] == code {
			return true
		}
	}
	return false
}

func readBody(response *http.Response) string {
	if response == nil {
		return ""
	}
	defer response.Body.Close()
	body, _ := io.ReadAll(response.Body)
	return string(body)
}

func generatedFiles(t *testing.T, root string) int {
	t.Helper()
	count := 0
	err := filepath.Walk(root, func(_ string, info os.FileInfo, err error) error {
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		if info != nil && !info.IsDir() {
			count++
		}
		return nil
	})
	if err != nil && !os.IsNotExist(err) {
		t.Fatalf("walk generated files: %v", err)
	}
	return count
}
