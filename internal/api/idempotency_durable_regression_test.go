package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mikkelchokolate/Veil/internal/storage"
)

type idempotencyProcessConfig struct {
	Root       string `json:"root"`
	Barrier    string `json:"barrier"`
	ResultPath string `json:"resultPath"`
	Body       string `json:"body"`
}

type idempotencyProcessResult struct {
	Status   int    `json:"status"`
	Body     string `json:"body"`
	Replayed string `json:"replayed"`
}

func TestIdempotencyReplayAndConflictSurviveRestart(t *testing.T) {
	info := durableIdempotencyServerInfo(t.TempDir())
	body := `{"panelListen":"127.0.0.1:2096","panelAccess":"","webBasePath":"","mode":"dev","domain":"restart-idem.example.com","email":"admin@example.com","firewallManagement":true}`
	first := runIdempotentSettingsRequest(t, info, "restart-key", body)
	if first.Status != http.StatusOK {
		t.Fatalf("first request: %+v", first)
	}
	second := runIdempotentSettingsRequest(t, info, "restart-key", body)
	if second.Status != first.Status || second.Body != first.Body || second.Replayed != "true" {
		t.Fatalf("restart replay mismatch: first=%+v second=%+v", first, second)
	}
	conflictBody := `{"panelListen":"127.0.0.1:2096","panelAccess":"","webBasePath":"","mode":"dev","domain":"different.example.com","email":"admin@example.com","firewallManagement":true}`
	conflict := runIdempotentSettingsRequest(t, info, "restart-key", conflictBody)
	if conflict.Status != http.StatusConflict || conflict.Replayed != "" {
		t.Fatalf("restart payload conflict not rejected: %+v", conflict)
	}
}

func TestIdempotencyReservationIsSharedAcrossOSProcesses(t *testing.T) {
	root := t.TempDir()
	info := durableIdempotencyServerInfo(root)
	// Seed key/state/database before concurrent opens.
	seedBody := `{"panelListen":"127.0.0.1:2096","panelAccess":"","webBasePath":"","mode":"dev","domain":"seed-idem.example.com","email":"admin@example.com","firewallManagement":true}`
	seed := runIdempotentSettingsRequest(t, info, "seed-key", seedBody)
	if seed.Status != http.StatusOK {
		t.Fatalf("seed request: %+v", seed)
	}
	databasePath := findManagementDatabase(t, root)
	baseDesired := readDesiredRevisionFromDB(t, databasePath)

	barrier := filepath.Join(root, "start")
	body := `{"panelListen":"127.0.0.1:2096","panelAccess":"","webBasePath":"","mode":"dev","domain":"multiprocess-idem.example.com","email":"admin@example.com","firewallManagement":true}`
	commands := make([]*exec.Cmd, 2)
	outputs := make([]bytes.Buffer, 2)
	results := make([]string, 2)
	for index := range commands {
		results[index] = filepath.Join(root, fmt.Sprintf("result-%d.json", index))
		config := idempotencyProcessConfig{Root: root, Barrier: barrier, ResultPath: results[index], Body: body}
		raw, err := json.Marshal(config)
		if err != nil {
			t.Fatal(err)
		}
		commands[index] = exec.Command(os.Args[0], "-test.run=^TestIdempotencyProcessHelper$")
		commands[index].Env = append(os.Environ(), "VEIL_IDEMPOTENCY_PROCESS="+string(raw))
		commands[index].Stdout = &outputs[index]
		commands[index].Stderr = &outputs[index]
		if err := commands[index].Start(); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(barrier, []byte("go"), 0o600); err != nil {
		t.Fatal(err)
	}
	for index, command := range commands {
		if err := command.Wait(); err != nil {
			t.Fatalf("idempotency child: %v\n%s", err, outputs[index].String())
		}
	}
	var observed []idempotencyProcessResult
	for _, path := range results {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var result idempotencyProcessResult
		if err := json.Unmarshal(raw, &result); err != nil {
			t.Fatal(err)
		}
		observed = append(observed, result)
	}
	if observed[0].Status != http.StatusOK || observed[1].Status != http.StatusOK || observed[0].Body != observed[1].Body {
		t.Fatalf("multi-process responses differ: %+v", observed)
	}
	replays := 0
	for _, result := range observed {
		if result.Replayed == "true" {
			replays++
		}
	}
	if replays != 1 {
		t.Fatalf("multi-process replay count=%d want=1 results=%+v", replays, observed)
	}
	if desired := readDesiredRevisionFromDB(t, databasePath); desired != baseDesired+1 {
		t.Fatalf("desired revision=%d want=%d; duplicate process executed side effect", desired, baseDesired+1)
	}
}

func TestIdempotencyProcessHelper(t *testing.T) {
	raw := os.Getenv("VEIL_IDEMPOTENCY_PROCESS")
	if raw == "" {
		t.Skip("subprocess helper")
	}
	var config idempotencyProcessConfig
	if err := json.Unmarshal([]byte(raw), &config); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := os.Stat(config.Barrier); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("barrier timeout")
		}
		time.Sleep(10 * time.Millisecond)
	}
	autoApplyAfterMutation = false
	result := runIdempotentSettingsRequest(t, durableIdempotencyServerInfo(config.Root), "multiprocess-key", config.Body)
	body, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(config.ResultPath, body, 0o600); err != nil {
		t.Fatal(err)
	}
}

func durableIdempotencyServerInfo(root string) ServerInfo {
	return ServerInfo{
		Version:   "test",
		Mode:      "dev",
		AuthToken: "idempotency-test-token",
		StatePath: filepath.Join(root, "state.json"),
		KeyPath:   filepath.Join(root, "state.key"),
		ApplyRoot: filepath.Join(root, "apply"),
		LiveRoot:  filepath.Join(root, "live"),
	}
}

func runIdempotentSettingsRequest(t *testing.T, info ServerInfo, key, body string) idempotencyProcessResult {
	t.Helper()
	previousAutoApply := autoApplyAfterMutation
	autoApplyAfterMutation = false
	defer func() { autoApplyAfterMutation = previousAutoApply }()
	handler, reloader := NewRouter(info)
	request := httptest.NewRequest(http.MethodPut, "/api/settings", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Veil-Token", info.AuthToken)
	request.Header.Set("Idempotency-Key", key)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	closeRouterState(t, reloader)
	return idempotencyProcessResult{Status: response.Code, Body: response.Body.String(), Replayed: response.Header().Get("Idempotency-Replayed")}
}

func closeRouterState(t *testing.T, reloader Reloader) {
	t.Helper()
	if closer, ok := reloader.(interface{ Close() error }); ok {
		if err := closer.Close(); err != nil {
			t.Fatalf("close router state: %v", err)
		}
	}
}

func findManagementDatabase(t *testing.T, root string) string {
	t.Helper()
	var found string
	var seen []string
	err := filepath.Walk(root, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if info.IsDir() {
			return nil
		}
		seen = append(seen, path)
		if filepath.Ext(path) != ".db" {
			return nil
		}
		db, err := storage.OpenExisting(path)
		if err != nil {
			return nil
		}
		var exists int
		err = db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='revisions'`).Scan(&exists)
		_ = db.Close()
		if err == nil && exists == 1 {
			found = path
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if found == "" {
		t.Fatalf("management database with apply_revision_state not found; files=%v", seen)
	}
	return found
}

func readDesiredRevisionFromDB(t *testing.T, path string) uint64 {
	t.Helper()
	db, err := storage.OpenExisting(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var desired uint64
	if err := db.QueryRow(`SELECT desired_revision FROM revisions WHERE id=1`).Scan(&desired); err != nil {
		t.Fatal(err)
	}
	return desired
}
