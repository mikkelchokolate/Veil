package privileged

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

const promotionCrashHelperEnv = "VEIL_PROMOTION_CRASH_HELPER"

func TestPromotionOrdinaryErrorRollsBackEveryPreviouslyChangedArtifact(t *testing.T) {
	root := t.TempDir()
	request := preparePromotionFixture(t, root, 3)
	request.Artifacts[1].Source = filepath.Join(root, "missing-second-source")
	withStubbedArtifactOwnership(t, func() {
		if _, err := promoteResolvedArtifacts(filepath.Join(root, "backups"), fixedPromotionNow, request); err == nil {
			t.Fatal("expected injected second-artifact error")
		}
	})
	assertPromotionSet(t, request, "old")
}

func TestPromotionRecoversSIGKILLAfterEveryArtifactPublication(t *testing.T) {
	for faultArtifact := 1; faultArtifact <= 3; faultArtifact++ {
		t.Run(fmt.Sprintf("after-artifact-%d", faultArtifact), func(t *testing.T) {
			root := t.TempDir()
			request := preparePromotionFixture(t, root, 3)
			runPromotionCrashHelper(t, root, "promote", "", faultArtifact)

			// Re-entering the privileged promotion subsystem represents helper
			// startup/recovery before another operation is accepted.
			withStubbedArtifactOwnership(t, func() {
				if _, err := promoteResolvedArtifacts(filepath.Join(root, "backups"), fixedPromotionNow, ResolvedPromotion{}); err != nil {
					t.Fatalf("recover interrupted promotion: %v", err)
				}
			})
			state := classifyPromotionSet(t, request)
			if state == "mixed" {
				t.Fatal("interrupted promotion recovery left a mixed live artifact set")
			}
			if state == "new" {
				manifest := filepath.Join(root, "backups", fixedPromotionBackupID(), "manifest.json")
				if _, err := os.Stat(manifest); err != nil {
					t.Fatalf("new set became visible without durable committed manifest: %v", err)
				}
			}
		})
	}
}

func TestPromotionRollbackRecoversSIGKILLAfterEveryArtifactPublication(t *testing.T) {
	for faultArtifact := 1; faultArtifact <= 3; faultArtifact++ {
		t.Run(fmt.Sprintf("after-artifact-%d", faultArtifact), func(t *testing.T) {
			root := t.TempDir()
			request := preparePromotionFixture(t, root, 3)
			var promoted PromoteResult
			withStubbedArtifactOwnership(t, func() {
				var err error
				promoted, err = promoteResolvedArtifacts(filepath.Join(root, "backups"), fixedPromotionNow, request)
				if err != nil {
					t.Fatalf("initial promotion: %v", err)
				}
			})
			assertPromotionSet(t, request, "new")

			runPromotionCrashHelper(t, root, "rollback", promoted.BackupID, faultArtifact)
			withStubbedArtifactOwnership(t, func() {
				if _, err := promoteResolvedArtifacts(filepath.Join(root, "backups"), fixedPromotionNow, ResolvedPromotion{}); err != nil {
					t.Fatalf("recover interrupted rollback: %v", err)
				}
			})
			if state := classifyPromotionSet(t, request); state == "mixed" {
				t.Fatal("interrupted rollback recovery left a mixed live artifact set")
			}
		})
	}
}

func TestPromotionManifestContainsDurableTransactionEvidence(t *testing.T) {
	root := t.TempDir()
	request := preparePromotionFixture(t, root, 2)
	var result PromoteResult
	withStubbedArtifactOwnership(t, func() {
		var err error
		result, err = promoteResolvedArtifacts(filepath.Join(root, "backups"), fixedPromotionNow, request)
		if err != nil {
			t.Fatal(err)
		}
	})
	body, err := os.ReadFile(filepath.Join(root, "backups", result.BackupID, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	var manifest map[string]any
	if err := json.Unmarshal(body, &manifest); err != nil {
		t.Fatal(err)
	}
	transactionID, hasTransactionID := manifest["transactionId"]
	if !hasTransactionID || transactionID == nil || strings.TrimSpace(fmt.Sprint(transactionID)) == "" {
		t.Error("promotion manifest omits transactionId")
	}
	records, _ := manifest["records"].([]any)
	if len(records) != len(request.Artifacts) {
		t.Fatalf("manifest records=%d want=%d", len(records), len(request.Artifacts))
	}
	for i, raw := range records {
		record, _ := raw.(map[string]any)
		for _, field := range []string{"oldDigest", "newDigest", "safetyPath", "phase"} {
			value, exists := record[field]
			if !exists || value == nil || strings.TrimSpace(fmt.Sprint(value)) == "" {
				t.Errorf("record %d omits %s: %#v", i, field, record)
			}
		}
	}
}

func TestPromotionCrashProcess(t *testing.T) {
	if os.Getenv(promotionCrashHelperEnv) != "1" {
		t.Skip("subprocess helper")
	}
	root := os.Getenv("VEIL_PROMOTION_ROOT")
	mode := os.Getenv("VEIL_PROMOTION_MODE")
	backupID := os.Getenv("VEIL_PROMOTION_BACKUP_ID")
	faultArtifact, err := strconv.Atoi(os.Getenv("VEIL_PROMOTION_FAULT_ARTIFACT"))
	if err != nil || faultArtifact < 1 {
		os.Exit(92)
	}
	marker := filepath.Join(root, "crash-marker")
	request := promotionRequestForRoot(root, 3)
	originalEffectiveUID := effectiveUID
	originalLookupGroup := lookupGroup
	originalChownPath := chownPath
	defer func() {
		effectiveUID = originalEffectiveUID
		lookupGroup = originalLookupGroup
		chownPath = originalChownPath
	}()
	effectiveUID = func() int { return 0 }
	lookupGroup = func(string) (*user.Group, error) { return &user.Group{Gid: "0"}, nil }
	chownCalls := 0
	chownPath = func(string, int, int) error {
		chownCalls++
		// Non-Caddy artifacts call chown(directory), chown(generated root),
		// chown(file). Block on the directory ownership step immediately
		// after each file rename.
		if chownCalls == (faultArtifact-1)*3+1 {
			if err := os.WriteFile(marker, []byte("published"), 0o600); err != nil {
				os.Exit(93)
			}
			if os.Getenv("VEIL_PROMOTION_READY") == "1" {
				ready := os.NewFile(3, "promotion-crash-ready")
				if ready == nil {
					os.Exit(94)
				}
				if _, err := ready.Write([]byte{1}); err != nil {
					os.Exit(94)
				}
				if err := ready.Close(); err != nil {
					os.Exit(94)
				}
			}
			select {}
		}
		return nil
	}

	switch mode {
	case "promote":
		_, _ = promoteResolvedArtifacts(filepath.Join(root, "backups"), fixedPromotionNow, request)
	case "rollback":
		_, _ = restorePromotedArtifacts(filepath.Join(root, "backups"), backupID)
	default:
		os.Exit(94)
	}
	os.Exit(95) // the fault hook must prevent normal return
}

func runPromotionCrashHelper(t *testing.T, root, mode, backupID string, faultArtifact int) {
	t.Helper()
	command := exec.Command(os.Args[0], "-test.run=^TestPromotionCrashProcess$")
	readyRead, readyWrite, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer readyRead.Close()
	command.ExtraFiles = []*os.File{readyWrite}
	command.Env = append(os.Environ(),
		promotionCrashHelperEnv+"=1",
		"VEIL_PROMOTION_READY=1",
		"VEIL_PROMOTION_ROOT="+root,
		"VEIL_PROMOTION_MODE="+mode,
		"VEIL_PROMOTION_BACKUP_ID="+backupID,
		"VEIL_PROMOTION_FAULT_ARTIFACT="+strconv.Itoa(faultArtifact),
	)
	var output strings.Builder
	command.Stdout = &output
	command.Stderr = &output
	if err := command.Start(); err != nil {
		readyWrite.Close()
		t.Fatal(err)
	}
	_ = readyWrite.Close()
	ready := make(chan error, 1)
	go func() {
		_, readErr := io.ReadFull(readyRead, make([]byte, 1))
		ready <- readErr
	}()
	select {
	case err := <-ready:
		if err != nil {
			_ = command.Process.Kill()
			_, _ = command.Process.Wait()
			t.Fatalf("crash helper did not reach artifact %d: %v: %s", faultArtifact, err, output.String())
		}
	case <-time.After(10 * time.Second):
		_ = command.Process.Kill()
		_, _ = command.Process.Wait()
		t.Fatalf("crash helper did not reach artifact %d: %s", faultArtifact, output.String())
	}
	if err := command.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_, _ = command.Process.Wait()
}

func preparePromotionFixture(t *testing.T, root string, count int) ResolvedPromotion {
	t.Helper()
	request := promotionRequestForRoot(root, count)
	for i, artifact := range request.Artifacts {
		if err := os.MkdirAll(filepath.Dir(artifact.Source), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(filepath.Dir(artifact.Destination), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(artifact.Source, []byte(fmt.Sprintf("new-%d", i+1)), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(artifact.Destination, []byte(fmt.Sprintf("old-%d", i+1)), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return request
}

func promotionRequestForRoot(root string, count int) ResolvedPromotion {
	request := ResolvedPromotion{}
	for i := 1; i <= count; i++ {
		request.Artifacts = append(request.Artifacts, ResolvedArtifact{
			ID:          fmt.Sprintf("mieru/config-%d.json", i),
			Source:      filepath.Join(root, "staged", fmt.Sprintf("config-%d.json", i)),
			Destination: filepath.Join(root, "live", fmt.Sprintf("config-%d.json", i)),
		})
	}
	return request
}

func fixedPromotionNow() time.Time {
	return time.Date(2026, time.July, 27, 12, 34, 56, 789000000, time.UTC)
}

func fixedPromotionBackupID() string {
	return fixedPromotionNow().UTC().Format("20060102T150405.000000000Z")
}

// Distinct fake group IDs so tests can prove promotion chowns to the resolved
// veil-proxy group (preferred) rather than any coincidental value like the
// process gid or the veil fallback group.
const (
	promotionTestProxyGID = 977
	promotionTestVeilGID  = 978
)

type recordedOwnership struct {
	chowns []recordedChown
	chmods []recordedChmod
}

type recordedChown struct {
	path string
	uid  int
	gid  int
}

type recordedChmod struct {
	path string
	mode os.FileMode
}

// withStubbedArtifactOwnership runs the promotion transaction machinery as
// root with veil/veil-proxy resolvable so the ownership enforcement path
// (#522) executes, while the chown/chmod syscalls themselves are recorded
// no-ops. Callers that need to prove the ownership contract use
// withRecordingArtifactOwnership and assert the recorded calls.
func withStubbedArtifactOwnership(t *testing.T, run func()) {
	t.Helper()
	withRecordingArtifactOwnership(t, func(*recordedOwnership) { run() })
}

// withRecordingArtifactOwnership is withStubbedArtifactOwnership plus a
// recorder: every chown/chmod the transaction issues is captured so tests can
// assert target paths, uid 0, the veil-proxy gid, and the 0750/0640 modes.
func withRecordingArtifactOwnership(t *testing.T, run func(*recordedOwnership)) {
	t.Helper()
	originalEffectiveUID := effectiveUID
	originalLookupGroup := lookupGroup
	originalChownPath := chownPath
	originalChmodPath := chmodPath
	recorded := &recordedOwnership{}
	effectiveUID = func() int { return 0 }
	lookupGroup = func(name string) (*user.Group, error) {
		switch name {
		case "veil-proxy":
			return &user.Group{Gid: strconv.Itoa(promotionTestProxyGID)}, nil
		case "veil":
			return &user.Group{Gid: strconv.Itoa(promotionTestVeilGID)}, nil
		default:
			return nil, fmt.Errorf("unknown group %q", name)
		}
	}
	chownPath = func(path string, uid, gid int) error {
		recorded.chowns = append(recorded.chowns, recordedChown{path: path, uid: uid, gid: gid})
		return nil
	}
	chmodPath = func(path string, mode os.FileMode) error {
		recorded.chmods = append(recorded.chmods, recordedChmod{path: path, mode: mode})
		return nil
	}
	defer func() {
		effectiveUID = originalEffectiveUID
		lookupGroup = originalLookupGroup
		chownPath = originalChownPath
		chmodPath = originalChmodPath
	}()
	run(recorded)
}

func TestPromotionAppliesRuntimeArtifactOwnershipContract(t *testing.T) {
	root := t.TempDir()
	request := preparePromotionFixture(t, root, 2)
	var recorded *recordedOwnership
	withRecordingArtifactOwnership(t, func(r *recordedOwnership) {
		recorded = r
		if _, err := promoteResolvedArtifacts(filepath.Join(root, "backups"), fixedPromotionNow, request); err != nil {
			t.Fatal(err)
		}
	})

	// Each artifact id is "mieru/config-N.json", so the contract is: artifact
	// directory 0:veil-proxy 0750, generated root 0:veil-proxy 0750, then the
	// file 0:veil-proxy 0640 — in that order, per artifact.
	type wantCall struct {
		path string
		gid  int
		mode os.FileMode
	}
	var want []wantCall
	for _, artifact := range request.Artifacts {
		dir := filepath.Dir(artifact.Destination)
		generatedRoot := filepath.Dir(dir)
		want = append(want,
			wantCall{dir, promotionTestProxyGID, 0o750},
			wantCall{generatedRoot, promotionTestProxyGID, 0o750},
			wantCall{artifact.Destination, promotionTestProxyGID, 0o640},
		)
	}
	if len(recorded.chowns) != len(want) || len(recorded.chmods) != len(want) {
		t.Fatalf("ownership calls: %d chowns %d chmods, want %d each: chowns=%+v chmods=%+v",
			len(recorded.chowns), len(recorded.chmods), len(want), recorded.chowns, recorded.chmods)
	}
	for i, expected := range want {
		if got := recorded.chowns[i]; got.path != expected.path || got.uid != 0 || got.gid != expected.gid {
			t.Errorf("chown[%d] = %+v, want path=%s uid=0 gid=%d", i, got, expected.path, expected.gid)
		}
		if got := recorded.chmods[i]; got.path != expected.path || got.mode != expected.mode {
			t.Errorf("chmod[%d] = %+v, want path=%s mode=%o", i, got, expected.path, expected.mode)
		}
	}
}

func classifyPromotionSet(t *testing.T, request ResolvedPromotion) string {
	t.Helper()
	oldCount, newCount := 0, 0
	for i, artifact := range request.Artifacts {
		body, err := os.ReadFile(artifact.Destination)
		if err != nil {
			t.Fatalf("read %s: %v", artifact.Destination, err)
		}
		switch string(body) {
		case fmt.Sprintf("old-%d", i+1):
			oldCount++
		case fmt.Sprintf("new-%d", i+1):
			newCount++
		default:
			t.Fatalf("unexpected artifact %s body %q", artifact.ID, body)
		}
	}
	if oldCount == len(request.Artifacts) {
		return "old"
	}
	if newCount == len(request.Artifacts) {
		return "new"
	}
	return "mixed"
}

func assertPromotionSet(t *testing.T, request ResolvedPromotion, want string) {
	t.Helper()
	if got := classifyPromotionSet(t, request); got != want {
		t.Fatalf("promotion set=%s want=%s", got, want)
	}
}
