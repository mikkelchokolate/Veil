package privileged

import (
	"encoding/json"
	"fmt"
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
	withNonRootPromotionHooks(t, func() {
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
			withNonRootPromotionHooks(t, func() {
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
			withNonRootPromotionHooks(t, func() {
				var err error
				promoted, err = promoteResolvedArtifacts(filepath.Join(root, "backups"), fixedPromotionNow, request)
				if err != nil {
					t.Fatalf("initial promotion: %v", err)
				}
			})
			assertPromotionSet(t, request, "new")

			runPromotionCrashHelper(t, root, "rollback", promoted.BackupID, faultArtifact)
			withNonRootPromotionHooks(t, func() {
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
	withNonRootPromotionHooks(t, func() {
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
		return
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
	originalLookupUser := lookupUser
	originalChownPath := chownPath
	defer func() {
		effectiveUID = originalEffectiveUID
		lookupUser = originalLookupUser
		chownPath = originalChownPath
	}()
	effectiveUID = func() int { return 0 }
	lookupUser = func(string) (*user.User, error) { return &user.User{Uid: "0", Gid: "0"}, nil }
	chownCalls := 0
	chownPath = func(string, int, int) error {
		chownCalls++
		// Non-Caddy artifacts call chown(directory), chown(file). Block on
		// the directory ownership step immediately after each file rename.
		if chownCalls == (faultArtifact-1)*2+1 {
			if err := os.WriteFile(marker, []byte("published"), 0o600); err != nil {
				os.Exit(93)
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
	command.Env = append(os.Environ(),
		promotionCrashHelperEnv+"=1",
		"VEIL_PROMOTION_ROOT="+root,
		"VEIL_PROMOTION_MODE="+mode,
		"VEIL_PROMOTION_BACKUP_ID="+backupID,
		"VEIL_PROMOTION_FAULT_ARTIFACT="+strconv.Itoa(faultArtifact),
	)
	var output strings.Builder
	command.Stdout = &output
	command.Stderr = &output
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(root, "crash-marker")
	deadline := time.Now().Add(10 * time.Second)
	for {
		if _, err := os.Stat(marker); err == nil {
			break
		}
		if time.Now().After(deadline) {
			_ = command.Process.Kill()
			_, _ = command.Process.Wait()
			t.Fatalf("crash helper did not reach artifact %d: %s", faultArtifact, output.String())
		}
		time.Sleep(10 * time.Millisecond)
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

func withNonRootPromotionHooks(t *testing.T, run func()) {
	t.Helper()
	original := effectiveUID
	effectiveUID = func() int { return 1000 }
	defer func() { effectiveUID = original }()
	run()
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
