package apply

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/mikkelchokolate/Veil/internal/storage"
)

type fencingSubprocessResult struct {
	JobID  string `json:"jobId"`
	Status string `json:"status"`
	Error  string `json:"error,omitempty"`
}

func TestApplyFencingAcrossOSProcesses(t *testing.T) {
	if os.Getenv("VEIL_APPLY_FENCE_CHILD") != "" {
		t.Skip("parent-only test")
	}
	root := t.TempDir()
	dbPath := filepath.Join(root, "fencing.db")
	db, err := storage.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	revision, err := NewRevisionStore(db).BumpDesired()
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	started := filepath.Join(root, "a.started")
	resume := filepath.Join(root, "a.resume")
	resultA := filepath.Join(root, "a.json")
	resultB := filepath.Join(root, "b.json")

	child := func(owner, result string) *exec.Cmd {
		cmd := exec.Command(os.Args[0], "-test.run=^TestApplyFenceSubprocessHelper$", "-test.v")
		cmd.Env = append(os.Environ(),
			"VEIL_APPLY_FENCE_CHILD="+owner,
			"VEIL_APPLY_FENCE_DB="+dbPath,
			"VEIL_APPLY_FENCE_STARTED="+started,
			"VEIL_APPLY_FENCE_RESUME="+resume,
			"VEIL_APPLY_FENCE_RESULT="+result,
		)
		return cmd
	}

	processA := child("process-a", resultA)
	if err := processA.Start(); err != nil {
		t.Fatal(err)
	}
	waitForFenceFile(t, started, 5*time.Second)
	// Lease timestamps are persisted at whole-second precision. Waiting over two
	// seconds makes expiry deterministic across scheduler and wall-clock boundaries.
	time.Sleep(2200 * time.Millisecond)
	processB := child("process-b", resultB)
	if output, err := processB.CombinedOutput(); err != nil {
		_ = os.WriteFile(resume, []byte("resume"), 0o600)
		_ = processA.Wait()
		t.Fatalf("process B: %v\n%s", err, output)
	}
	if err := os.WriteFile(resume, []byte("resume"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := processA.Wait(); err != nil {
		t.Fatalf("process A helper process: %v", err)
	}

	a := readFenceSubprocessResult(t, resultA)
	b := readFenceSubprocessResult(t, resultB)
	if b.Status != StatusSucceeded || b.Error != "" {
		t.Errorf("successor process B = %+v, want sole success", b)
	}
	if a.Status == StatusSucceeded || a.Error == "" {
		t.Errorf("expired process A escaped fencing: %+v", a)
	}

	db, err = storage.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var generation uint64
	if err := db.QueryRow(`SELECT generation FROM apply_lease WHERE id=1`).Scan(&generation); err != nil {
		t.Errorf("load monotonic lease generation: %v", err)
	} else if generation < 2 {
		t.Errorf("lease generation = %d, want at least 2", generation)
	}
	state, err := NewRevisionStore(db).Get()
	if err != nil {
		t.Fatal(err)
	}
	if state.Applied != revision {
		t.Errorf("applied revision = %d, want successor revision %d", state.Applied, revision)
	}
}

func TestApplyFenceSubprocessHelper(t *testing.T) {
	owner := os.Getenv("VEIL_APPLY_FENCE_CHILD")
	if owner == "" {
		t.Skip("subprocess helper")
	}
	db, err := storage.Open(os.Getenv("VEIL_APPLY_FENCE_DB"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	revisions, jobs := NewRevisionStore(db), NewJobStore(db)
	state, err := revisions.Get()
	if err != nil {
		t.Fatal(err)
	}
	runner := NewRunner(revisions, jobs, ContextExecutorFunc(func(_ context.Context, _ uint64) (Result, error) {
		if owner == "process-a" {
			if err := os.WriteFile(os.Getenv("VEIL_APPLY_FENCE_STARTED"), []byte("started"), 0o600); err != nil {
				return Result{}, err
			}
			deadline := time.Now().Add(10 * time.Second)
			for {
				if _, err := os.Stat(os.Getenv("VEIL_APPLY_FENCE_RESUME")); err == nil {
					break
				}
				if time.Now().After(deadline) {
					return Result{}, errors.New("timed out waiting for successor")
				}
				time.Sleep(10 * time.Millisecond)
			}
		}
		return Result{Success: true, Operations: []OperationResult{{Type: "runtime", Target: owner, Success: true}}}, nil
	}))
	runner.ownerID = owner
	runner.leaseTTL = time.Second
	runner.heartbeatInterval = time.Hour
	job, runErr := runner.RunContext(context.Background(), state.Desired, "multiprocess", owner)
	result := fencingSubprocessResult{JobID: job.ID, Status: job.Status}
	if runErr != nil {
		result.Error = runErr.Error()
	}
	payload, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(os.Getenv("VEIL_APPLY_FENCE_RESULT"), payload, 0o600); err != nil {
		t.Fatal(err)
	}
}

func waitForFenceFile(t *testing.T, path string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		if _, err := os.Stat(path); err == nil {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", path)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func readFenceSubprocessResult(t *testing.T, path string) fencingSubprocessResult {
	t.Helper()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var result fencingSubprocessResult
	if err := json.Unmarshal(body, &result); err != nil {
		t.Fatal(err)
	}
	return result
}
