package privileged

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestPolicyAllowsManagedUnitsAndRejectsCraftedUnits(t *testing.T) {
	policy := testPolicy(t)
	for _, unit := range []string{"veil.service", "veil-mieru.service"} {
		if err := policy.ValidateServiceAction(ServiceActionRequest{Unit: unit, Action: ServiceActionRestart}); err != nil {
			t.Fatalf("managed unit %q rejected: %v", unit, err)
		}
	}
	for _, unit := range []string{"ssh.service", "veil.service; reboot", "../veil.service", ""} {
		err := policy.ValidateServiceAction(ServiceActionRequest{Unit: unit, Action: ServiceActionRestart})
		assertOperationErrorCode(t, err, ErrorForbiddenOperation)
	}
}

func TestPolicyClampsJournalLinesAndRejectsUnknownUnit(t *testing.T) {
	policy := testPolicy(t)
	for input, want := range map[int]int{-10: 1, 0: 1, 1: 1, 500: 500, 5000: 1000} {
		resolved, err := policy.ResolveJournal(JournalRequest{Unit: "veil.service", Lines: input})
		if err != nil {
			t.Fatalf("resolve journal lines %d: %v", input, err)
		}
		if resolved.Lines != want {
			t.Errorf("lines %d: want %d, got %d", input, want, resolved.Lines)
		}
	}
	_, err := policy.ResolveJournal(JournalRequest{Unit: "unmanaged.service", Lines: 10})
	assertOperationErrorCode(t, err, ErrorForbiddenOperation)
}

func TestPolicyRequiresEncryptedBackupBasename(t *testing.T) {
	policy := testPolicy(t)
	for _, name := range []string{"daily.enc", "veil-20260605.enc"} {
		if _, err := policy.ResolveBackup(BackupRequest{Action: BackupActionVerify, ArchiveName: name}); err != nil {
			t.Fatalf("valid archive %q rejected: %v", name, err)
		}
	}
	for _, name := range []string{"", "daily.tar", "../daily.enc", "subdir/daily.enc", `C:\daily.enc`} {
		_, err := policy.ResolveBackup(BackupRequest{Action: BackupActionVerify, ArchiveName: name})
		assertOperationErrorCode(t, err, ErrorInvalidRequest)
	}
}

func TestPolicyResolvesArtifactIDsInsideManagedRoots(t *testing.T) {
	policy := testPolicy(t)
	resolved, err := policy.ResolvePromotion(PromoteRequest{ArtifactIDs: []string{"mieru"}})
	if err != nil {
		t.Fatalf("resolve promotion: %v", err)
	}
	if len(resolved.Artifacts) != 1 {
		t.Fatalf("expected one artifact, got %+v", resolved.Artifacts)
	}
	wantSource := filepath.Join(policy.StagingRoot, "mieru", "server_config.json")
	wantDestination := filepath.Join(policy.GeneratedRoot, "mieru", "server_config.json")
	if resolved.Artifacts[0].Source != wantSource || resolved.Artifacts[0].Destination != wantDestination {
		t.Fatalf("unexpected resolved artifact: %+v", resolved.Artifacts[0])
	}

	_, err = policy.ResolvePromotion(PromoteRequest{ArtifactIDs: []string{"unknown"}})
	assertOperationErrorCode(t, err, ErrorNotFound)
}

func TestPolicyRejectsArtifactTraversalAndSymlinkEscapes(t *testing.T) {
	policy := testPolicy(t)
	policy.Artifacts["traversal"] = ArtifactPath{
		Staged:    filepath.Join("..", "outside"),
		Generated: filepath.Join("..", "outside"),
	}
	if _, err := policy.ResolvePromotion(PromoteRequest{ArtifactIDs: []string{"traversal"}}); err == nil {
		t.Fatal("expected traversal artifact rejection")
	}

	outside := t.TempDir()
	link := filepath.Join(policy.StagingRoot, "escape")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symlink creation unavailable: %v", err)
	}
	policy.Artifacts["symlink"] = ArtifactPath{
		Staged:    filepath.Join("escape", "config.json"),
		Generated: filepath.Join("mieru", "safe.json"),
	}
	_, err := policy.ResolvePromotion(PromoteRequest{ArtifactIDs: []string{"symlink"}})
	assertOperationErrorCode(t, err, ErrorForbiddenOperation)
}

func TestPolicyResolvesOnlyRegisteredUpdateArtifacts(t *testing.T) {
	policy := testPolicy(t)
	resolved, err := policy.ResolveUpdate(UpdateRequest{ArtifactID: "veil-linux-amd64", Version: "0.6.0"})
	if err != nil {
		t.Fatalf("resolve update: %v", err)
	}
	want := filepath.Join(policy.UpdateRoot, "veil-linux-amd64")
	if resolved.Path != want {
		t.Fatalf("want %q, got %q", want, resolved.Path)
	}

	_, err = policy.ResolveUpdate(UpdateRequest{ArtifactID: "../veil"})
	assertOperationErrorCode(t, err, ErrorNotFound)
}

func testPolicy(t *testing.T) Policy {
	t.Helper()
	root := t.TempDir()
	policy := Policy{
		StagingRoot:   filepath.Join(root, "staging"),
		GeneratedRoot: filepath.Join(root, "generated"),
		StateRoot:     filepath.Join(root, "state"),
		BackupRoot:    filepath.Join(root, "backups"),
		UpdateRoot:    filepath.Join(root, "updates"),
		ManagedUnits: map[string]struct{}{
			"veil.service":       {},
			"veil-mieru.service": {},
		},
		Artifacts: map[string]ArtifactPath{
			"mieru": {
				Staged:    filepath.Join("mieru", "server_config.json"),
				Generated: filepath.Join("mieru", "server_config.json"),
			},
		},
		UpdateArtifacts: map[string]string{
			"veil-linux-amd64": "veil-linux-amd64",
		},
		FirewallRules: map[string]struct{}{
			"allow-mieru-tcp": {},
		},
	}
	for _, dir := range []string{
		policy.StagingRoot,
		policy.GeneratedRoot,
		policy.StateRoot,
		policy.BackupRoot,
		policy.UpdateRoot,
		filepath.Join(policy.StagingRoot, "mieru"),
		filepath.Join(policy.GeneratedRoot, "mieru"),
	} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("create policy directory %s: %v", dir, err)
		}
	}
	return policy
}

func assertOperationErrorCode(t *testing.T, err error, code ErrorCode) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected %s error", code)
	}
	var operationError *Error
	if !errors.As(err, &operationError) {
		t.Fatalf("expected privileged Error, got %T: %v", err, err)
	}
	if operationError.Code != code {
		t.Fatalf("want error code %s, got %s: %v", code, operationError.Code, err)
	}
}
