package privileged

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestPolicyAllowsManagedUnitsAndRejectsCraftedUnits(t *testing.T) {
	policy := testPolicy(t)
	for _, unit := range []string{"veil.service", "veil-mieru.service"} {
		for _, action := range []ServiceAction{
			ServiceActionStart, ServiceActionStop, ServiceActionRestart,
			ServiceActionReload, ServiceActionEnable, ServiceActionDisable,
		} {
			if err := policy.ValidateServiceAction(ServiceActionRequest{Unit: unit, Action: action}); err != nil {
				t.Fatalf("managed unit %q action %q rejected: %v", unit, action, err)
			}
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
	if resolved.ChecksumsPath != filepath.Join(policy.UpdateRoot, "checksums.txt") {
		t.Fatalf("checksums path=%q", resolved.ChecksumsPath)
	}

	_, err = policy.ResolveUpdate(UpdateRequest{ArtifactID: "../veil", Version: "0.6.0"})
	assertOperationErrorCode(t, err, ErrorNotFound)
	_, err = policy.ResolveUpdate(UpdateRequest{ArtifactID: "veil-linux-amd64", Version: "latest"})
	assertOperationErrorCode(t, err, ErrorInvalidRequest)
}

func TestPolicyResolvesManagedDynamicArtifactIDs(t *testing.T) {
	policy := testPolicy(t)
	resolved, err := policy.ResolvePromotion(PromoteRequest{
		ArtifactIDs: []string{
			"caddy/edge.json",
			"hysteria2/udp-edge.yaml",
			"olcrtc/rtc-edge.yaml",
			"mieru/server_config.json",
			"sing-box/warp.json",
			"rules/geoip.dat",
			"rules/geosite.dat",
		},
	})
	if err != nil {
		t.Fatalf("resolve dynamic artifacts: %v", err)
	}
	if len(resolved.Artifacts) != 7 {
		t.Fatalf("resolved artifacts=%+v", resolved.Artifacts)
	}
}

func TestPolicyAllowsLegacyCaddyfileOnlyForRemoval(t *testing.T) {
	policy := testPolicy(t)
	for _, root := range []string{policy.StagingRoot, policy.GeneratedRoot} {
		if err := os.MkdirAll(filepath.Join(root, "caddy"), 0o755); err != nil {
			t.Fatal(err)
		}
	}

	resolved, err := policy.ResolvePromotion(PromoteRequest{
		RemoveArtifactIDs: []string{"caddy/legacy.Caddyfile"},
	})
	if err != nil {
		t.Fatalf("resolve legacy Caddyfile removal: %v", err)
	}
	if len(resolved.RemoveArtifacts) != 1 || resolved.RemoveArtifacts[0].ID != "caddy/legacy.Caddyfile" {
		t.Fatalf("resolved removal = %+v", resolved.RemoveArtifacts)
	}

	_, err = policy.ResolvePromotion(PromoteRequest{
		ArtifactIDs: []string{"caddy/legacy.Caddyfile"},
	})
	assertOperationErrorCode(t, err, ErrorNotFound)

	for _, id := range []string{
		"caddy/bad.name.Caddyfile",
		"caddy/../escape.Caddyfile",
		"caddy/sub/escape.Caddyfile",
	} {
		_, err := policy.ResolvePromotion(PromoteRequest{RemoveArtifactIDs: []string{id}})
		assertOperationErrorCode(t, err, ErrorNotFound)
	}
}

func TestPolicyAllowsOpaquePromotionRestoreID(t *testing.T) {
	policy := testPolicy(t)
	resolved, err := policy.ResolvePromotion(PromoteRequest{RestoreBackupID: "20260605T120000.000000000Z"})
	if err != nil {
		t.Fatalf("resolve restore: %v", err)
	}
	if resolved.RestoreBackupID != "20260605T120000.000000000Z" {
		t.Fatalf("restore backup id=%q", resolved.RestoreBackupID)
	}
	for _, invalid := range []string{"../escape", "/absolute", `bad\id`, ""} {
		if _, err := policy.ResolvePromotion(PromoteRequest{RestoreBackupID: invalid}); err == nil {
			t.Fatalf("expected restore id %q to fail", invalid)
		}
	}
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

func TestErrorStringHandlesNil(t *testing.T) {
	var e *Error
	if e.Error() != "" {
		t.Fatalf("nil Error should return empty string, got %q", e.Error())
	}
	if (&Error{Code: ErrorInvalidRequest, Message: "boom"}).Error() != "boom" {
		t.Fatal("Error.Error() should return Message")
	}
}

func TestPolicyValidateServiceActionRejectsUnsupportedAction(t *testing.T) {
	policy := testPolicy(t)
	err := policy.ValidateServiceAction(ServiceActionRequest{Unit: "veil.service", Action: ServiceAction("freeze")})
	assertOperationErrorCode(t, err, ErrorInvalidRequest)
}

func TestPolicyValidateServiceStatusRequiresAtLeastOneUnit(t *testing.T) {
	policy := testPolicy(t)
	err := policy.ValidateServiceStatus(ServiceStatusRequest{Units: []string{}})
	assertOperationErrorCode(t, err, ErrorInvalidRequest)
	err = policy.ValidateServiceStatus(ServiceStatusRequest{Units: []string{"veil.service", "unmanaged.service"}})
	assertOperationErrorCode(t, err, ErrorForbiddenOperation)
}

func TestPolicyResolvePromotionRejectsRestoreWithArtifacts(t *testing.T) {
	policy := testPolicy(t)
	_, err := policy.ResolvePromotion(PromoteRequest{RestoreBackupID: "20260605T120000.000000000Z", ArtifactIDs: []string{"mieru"}})
	assertOperationErrorCode(t, err, ErrorInvalidRequest)
	_, err = policy.ResolvePromotion(PromoteRequest{RestoreBackupID: "bad id"})
	assertOperationErrorCode(t, err, ErrorInvalidRequest)
}

func TestPolicyResolvePromotionRequiresAtLeastOneArtifact(t *testing.T) {
	policy := testPolicy(t)
	_, err := policy.ResolvePromotion(PromoteRequest{})
	assertOperationErrorCode(t, err, ErrorInvalidRequest)
}

func TestPolicyResolvePromotionRejectsDuplicateArtifacts(t *testing.T) {
	policy := testPolicy(t)
	_, err := policy.ResolvePromotion(PromoteRequest{ArtifactIDs: []string{"mieru", "mieru"}})
	assertOperationErrorCode(t, err, ErrorConflict)
}

func TestPolicyManagedArtifactPathEdgeCases(t *testing.T) {
	tests := []struct {
		id      string
		allowed bool
	}{
		{"caddy/edge.json", true},
		{"hysteria2/udp.yaml", true},
		{"olcrtc/rtc.yaml", true},
		{"mieru/server_config.json", true},
		{"sing-box/warp.json", true},
		{"caddy/edge.yaml", false},
		{"hysteria2/udp.json", false},
		{"caddy/bad!.json", false},
		{"unknown/file.yaml", false},
		{"single.yaml", false},
		{"caddy/../escape.Caddyfile", false},
		{"caddy/sub/dir.Caddyfile", false},
	}
	for _, tc := range tests {
		_, ok := Policy{}.managedArtifactPath(tc.id)
		if ok != tc.allowed {
			t.Errorf("managedArtifactPath(%q) got %v, want %v", tc.id, ok, tc.allowed)
		}
	}
}

func TestPolicyResolveBackupRejectsUnsupportedAction(t *testing.T) {
	policy := testPolicy(t)
	_, err := policy.ResolveBackup(BackupRequest{Action: BackupAction("purge")})
	assertOperationErrorCode(t, err, ErrorInvalidRequest)
}

func TestPolicyResolveBackupDefaultsPathsWhenEmpty(t *testing.T) {
	root := t.TempDir()
	policy := Policy{StateRoot: root, BackupRoot: root}
	resolved, err := policy.ResolveBackup(BackupRequest{Action: BackupActionCreate})
	if err != nil {
		t.Fatalf("resolve backup: %v", err)
	}
	if resolved.StatePath != filepath.Join(root, "state.json") {
		t.Fatalf("unexpected state path %q", resolved.StatePath)
	}
	if resolved.KeyPath != filepath.Join(root, "state.key") {
		t.Fatalf("unexpected key path %q", resolved.KeyPath)
	}
	if resolved.BackupPassphrasePath != filepath.Join(root, "backup.passphrase") {
		t.Fatalf("unexpected passphrase path %q", resolved.BackupPassphrasePath)
	}
}

func TestPolicyResolveBackupAllowsListAndPruneWithoutArchive(t *testing.T) {
	policy := testPolicy(t)
	for _, action := range []BackupAction{BackupActionList, BackupActionPrune} {
		if _, err := policy.ResolveBackup(BackupRequest{Action: action}); err != nil {
			t.Fatalf("action %q rejected: %v", action, err)
		}
	}
}

func TestPolicyResolveFirewall(t *testing.T) {
	policy := testPolicy(t)
	resolved, err := policy.ResolveFirewall(FirewallRequest{RuleIDs: []string{"allow-mieru-tcp"}})
	if err != nil {
		t.Fatalf("resolve firewall rule ids: %v", err)
	}
	if !reflect.DeepEqual(resolved.RuleIDs, []string{"allow-mieru-tcp"}) {
		t.Fatalf("unexpected rule ids: %+v", resolved.RuleIDs)
	}

	_, err = policy.ResolveFirewall(FirewallRequest{RuleIDs: []string{"allow-unknown"}})
	assertOperationErrorCode(t, err, ErrorForbiddenOperation)

	_, err = policy.ResolveFirewall(FirewallRequest{})
	assertOperationErrorCode(t, err, ErrorInvalidRequest)

	resolved, err = policy.ResolveFirewall(FirewallRequest{Rules: []FirewallRule{{Command: "ufw", Args: []string{"allow", "443/tcp", "comment", "HTTPS"}}}})
	if err != nil {
		t.Fatalf("resolve firewall rules: %v", err)
	}
	if len(resolved.Rules) != 1 {
		t.Fatalf("unexpected rules: %+v", resolved.Rules)
	}
	_, err = policy.ResolveFirewall(FirewallRequest{Rules: []FirewallRule{{Command: "ufw", Args: []string{"allow", "0/tcp", "comment", "bad"}}}})
	assertOperationErrorCode(t, err, ErrorInvalidRequest)
	_, err = policy.ResolveFirewall(FirewallRequest{Rules: []FirewallRule{{Command: "ufw", Args: []string{"allow", "99999/tcp", "comment", "bad"}}}})
	assertOperationErrorCode(t, err, ErrorInvalidRequest)
	_, err = policy.ResolveFirewall(FirewallRequest{Rules: []FirewallRule{{Command: "ufw", Args: []string{"allow", "443/tcp", "foo", "bar"}}}})
	assertOperationErrorCode(t, err, ErrorInvalidRequest)
	_, err = policy.ResolveFirewall(FirewallRequest{Rules: []FirewallRule{{Command: "ufw", Args: []string{"allow", "443/tcp", "; reboot"}}}})
	assertOperationErrorCode(t, err, ErrorInvalidRequest)
}

func TestPolicyValidateUFWRule(t *testing.T) {
	good := FirewallRule{Command: "ufw", Args: []string{"allow", "443/tcp", "comment", "HTTPS"}}
	if err := validateUFWRule(good); err != nil {
		t.Fatalf("valid rule rejected: %v", err)
	}
	bad := []FirewallRule{
		{Command: "iptables", Args: []string{"allow", "443/tcp"}},
		{Command: "ufw", Args: []string{"allow"}},
		{Command: "ufw", Args: []string{"deny", "443/tcp"}},
		{Command: "ufw", Args: []string{"allow", "notaport"}},
		{Command: "ufw", Args: []string{"allow", "443/tcp", "comment"}},
		{Command: "ufw", Args: []string{"allow", "443/tcp", "; reboot"}},
		{Command: "ufw", Args: []string{"allow", "443/tcp", "comment", "x", "extra"}},
		{Command: "ufw", Args: []string{"allow", "0/tcp"}},
		{Command: "ufw", Args: []string{"allow", "99999/tcp"}},
		{Command: "ufw", Args: []string{"allow", "443/tcp", "foo", "bar"}},
	}
	for _, rule := range bad {
		if err := validateUFWRule(rule); err == nil {
			t.Fatalf("expected rule %+v to be rejected", rule)
		}
	}
}

func TestPolicyResolveUpdateRejectsTraversal(t *testing.T) {
	policy := testPolicy(t)
	policy.UpdateArtifacts["traversal"] = "../escape.tar.gz"
	_, err := policy.ResolveUpdate(UpdateRequest{ArtifactID: "traversal", Version: "v1.0.0"})
	assertOperationErrorCode(t, err, ErrorForbiddenOperation)
}

func TestPolicyAllowsUnitPrefixesAndRejectsDangerousNames(t *testing.T) {
	policy := testPolicy(t)
	policy.ManagedUnitPrefixes = []string{"veil-hysteria2@"}
	if !policy.allowsUnit("veil-hysteria2@edge.service") {
		t.Fatal("expected prefix unit to be allowed")
	}
	for _, unit := range []string{"veil-hysteria2@edge.service; reboot", "veil-hysteria2@../edge.service", "veil-other@edge.service"} {
		if policy.allowsUnit(unit) {
			t.Fatalf("expected unit %q to be rejected", unit)
		}
	}
}

func TestPolicyResolveBelowRejectsBadInputs(t *testing.T) {
	root := t.TempDir()
	for _, tc := range []struct{ root, relative string }{
		{"", "file"},
		{root, ""},
		{root, "/absolute"},
		{root, ".."},
		{root, "../escape"},
	} {
		if _, err := resolveBelow(tc.root, tc.relative); err == nil {
			t.Fatalf("expected resolveBelow(%q, %q) to fail", tc.root, tc.relative)
		}
	}
}

func TestPolicyPathWithin(t *testing.T) {
	root := t.TempDir()
	if !pathWithin(root, filepath.Join(root, "sub")) {
		t.Fatal("expected subpath to be within root")
	}
	if pathWithin(root, filepath.Join(root, "..")) {
		t.Fatal("expected parent to be outside root")
	}
}

func TestWrapOperationError(t *testing.T) {
	if wrapOperationError(nil) != nil {
		t.Fatal("wrapOperationError(nil) should be nil")
	}
	privileged := newError(ErrorForbiddenOperation, "no")
	if wrapOperationError(privileged) != privileged {
		t.Fatal("wrapOperationError should preserve privileged Error")
	}
	err := wrapOperationError(errors.New("plain"))
	var opErr *Error
	if !errors.As(err, &opErr) || opErr.Code != ErrorOperationFailed {
		t.Fatalf("expected operation failed wrap, got %v", err)
	}
}
