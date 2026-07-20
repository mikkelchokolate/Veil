package api

import (
	"strings"
	"testing"

	"github.com/mikkelchokolate/Veil/internal/client"
	"github.com/mikkelchokolate/Veil/internal/model"
)

// TestPinnedRevisionRendersSnapshotCredentials is the A3 regression test:
// 1. Create revision N with client credential A.
// 2. Create revision N+1 with credential B (rotated).
// 3. Pin to revision N.
// 4. Prove the runtime render for revision N uses credential A, not B.
func TestPinnedRevisionRendersSnapshotCredentials(t *testing.T) {
	s := &managementState{}
	s.cipher = newTestCipher(t)
	s.settings = Settings{Domain: "x.example", PanelListen: "127.0.0.1:2096"}
	s.inbounds = []Inbound{{Name: "hy2", Protocol: "hysteria2", Transport: "udp", Port: 443, Enabled: true, Password: "inbound-pass"}}

	db := openApplyTestDB(t)
	repo := client.NewRepository(db)
	creds := client.NewCredentialStore(db, s.cipher)
	svc := client.NewService(repo, creds)
	s.clientService = svc
	s.clientRepo = repo

	// --- Revision N: client alice with credential A ---
	view, err := svc.Create(client.Client{Name: "alice", Enabled: true})
	if err != nil {
		t.Fatalf("create client: %v", err)
	}
	b, err := svc.AddBinding(view.ID, "hy2")
	if err != nil {
		t.Fatalf("add binding: %v", err)
	}
	if _, err := svc.SetCredential(b.ID, "password", "credential-A"); err != nil {
		t.Fatalf("set credential A: %v", err)
	}

	// Snapshot revision N (simulating bumpDesiredRevisionLocked).
	snapN := s.snapshotLocked()
	if len(snapN.Clients) != 1 || len(snapN.Bindings) != 1 || len(snapN.Credentials) != 1 {
		t.Fatalf("snapshot N missing client state: clients=%d bindings=%d creds=%d",
			len(snapN.Clients), len(snapN.Bindings), len(snapN.Credentials))
	}

	// --- Revision N+1: rotate credential to B ---
	if _, err := svc.RotateCredential(b.ID, "password", "credential-B"); err != nil {
		t.Fatalf("rotate credential: %v", err)
	}

	// Snapshot revision N+1.
	snapN1 := s.snapshotLocked()
	if len(snapN1.Credentials) != 1 {
		t.Fatalf("snapshot N+1 missing credentials: %d", len(snapN1.Credentials))
	}

	// Verify the two snapshots captured DIFFERENT credentials.
	if string(snapN.Credentials[0].EncryptedValue) == string(snapN1.Credentials[0].EncryptedValue) {
		t.Fatal("snapshots N and N+1 captured identical credentials — rotation not reflected")
	}

	// --- Pin to revision N and render ---
	applyRenderSnapshot(s, snapN)
	configs, err := s.renderManagementConfigsLocked()
	if err != nil {
		t.Fatalf("render revision N: %v", err)
	}

	foundA := false
	foundB := false
	for _, body := range configs {
		if strings.Contains(body, "credential-A") {
			foundA = true
		}
		if strings.Contains(body, "credential-B") {
			foundB = true
		}
	}
	if !foundA {
		t.Error("revision N render does NOT contain credential A — snapshot not applied")
	}
	if foundB {
		t.Error("revision N render contains credential B — VIOLATION: used newer mutable state")
	}

	// --- Pin to revision N+1 and render ---
	applyRenderSnapshot(s, snapN1)
	configs, err = s.renderManagementConfigsLocked()
	if err != nil {
		t.Fatalf("render revision N+1: %v", err)
	}

	foundA = false
	foundB = false
	for _, body := range configs {
		if strings.Contains(body, "credential-A") {
			foundA = true
		}
		if strings.Contains(body, "credential-B") {
			foundB = true
		}
	}
	if foundA {
		t.Error("revision N+1 render contains credential A — should have rotated to B")
	}
	if !foundB {
		t.Error("revision N+1 render does NOT contain credential B — rotation not applied")
	}
}

// TestSnapshotIncludesNormalizedClientState verifies (A3) that the immutable
// snapshot captures Clients, Bindings, and active Credentials — not just
// management JSON state.
func TestSnapshotIncludesNormalizedClientState(t *testing.T) {
	s := &managementState{}
	s.cipher = newTestCipher(t)
	s.settings = Settings{Domain: "x.example"}

	db := openApplyTestDB(t)
	repo := client.NewRepository(db)
	creds := client.NewCredentialStore(db, s.cipher)
	svc := client.NewService(repo, creds)
	s.clientService = svc
	s.clientRepo = repo

	view, err := svc.Create(client.Client{Name: "bob", Enabled: true, QuotaBytes: ptrInt64(1024)})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	b, err := svc.AddBinding(view.ID, "hy2")
	if err != nil {
		t.Fatalf("bind: %v", err)
	}
	if _, err := svc.SetCredential(b.ID, "password", "secret"); err != nil {
		t.Fatalf("cred: %v", err)
	}

	snap := s.snapshotLocked()

	if len(snap.Clients) != 1 {
		t.Fatalf("snapshot missing clients: %d", len(snap.Clients))
	}
	if snap.Clients[0].Name != "bob" {
		t.Errorf("client name mismatch: %q", snap.Clients[0].Name)
	}
	if snap.Clients[0].QuotaBytes == nil || *snap.Clients[0].QuotaBytes != 1024 {
		t.Errorf("client quota not captured: %v", snap.Clients[0].QuotaBytes)
	}

	if len(snap.Bindings) != 1 {
		t.Fatalf("snapshot missing bindings: %d", len(snap.Bindings))
	}
	if snap.Bindings[0].InboundID != "hy2" {
		t.Errorf("binding inbound mismatch: %q", snap.Bindings[0].InboundID)
	}

	if len(snap.Credentials) != 1 {
		t.Fatalf("snapshot missing credentials: %d", len(snap.Credentials))
	}
	if snap.Credentials[0].Kind != "password" {
		t.Errorf("credential kind mismatch: %q", snap.Credentials[0].Kind)
	}
	if len(snap.Credentials[0].EncryptedValue) == 0 {
		t.Error("credential encrypted value empty")
	}
	// Verify it's actually encrypted (not plaintext).
	if strings.Contains(string(snap.Credentials[0].EncryptedValue), "secret") {
		t.Error("credential stored as plaintext in snapshot")
	}
}

// TestPinnedSnapshotDoesNotFallBackToMutableState is the A3 immutability
// guarantee: when pinned to a revision, the renderer must NOT read current
// mutable SQLite state. Mutating the live store after pinning must not affect
// the pinned render.
func TestPinnedSnapshotDoesNotFallBackToMutableState(t *testing.T) {
	s := &managementState{}
	s.cipher = newTestCipher(t)
	s.settings = Settings{Domain: "x.example"}
	s.inbounds = []Inbound{{Name: "hy2", Protocol: "hysteria2", Transport: "udp", Port: 443, Enabled: true}}

	db := openApplyTestDB(t)
	repo := client.NewRepository(db)
	creds := client.NewCredentialStore(db, s.cipher)
	svc := client.NewService(repo, creds)
	s.clientService = svc
	s.clientRepo = repo

	// Create client with credential.
	view, _ := svc.Create(client.Client{Name: "carol", Enabled: true})
	b, _ := svc.AddBinding(view.ID, "hy2")
	svc.SetCredential(b.ID, "password", "original-pass")

	// Snapshot and pin.
	snap := s.snapshotLocked()
	applyRenderSnapshot(s, snap)

	// MUTATE live state AFTER pinning: delete the client entirely.
	svc.Delete(view.ID)

	// Render from pinned snapshot — must still see the client.
	inbounds := s.inboundsWithRuntimeCredentialsLocked()
	if len(inbounds) == 0 || len(inbounds[0].RuntimeCredentials) == 0 {
		t.Fatal("pinned render lost client credentials after live mutation — fell back to mutable state")
	}
	if inbounds[0].RuntimeCredentials[0].Name != "carol" {
		t.Errorf("pinned render wrong client: %q", inbounds[0].RuntimeCredentials[0].Name)
	}
	if inbounds[0].RuntimeCredentials[0].Password != "original-pass" {
		t.Errorf("pinned render wrong password: %q", inbounds[0].RuntimeCredentials[0].Password)
	}
}

func ptrInt64(v int64) *int64 { return &v }

// Ensure model import is used.
var _ = model.ManagementSnapshot{}
