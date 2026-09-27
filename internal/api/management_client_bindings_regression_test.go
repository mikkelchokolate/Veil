package api

import (
	"strings"
	"testing"

	"github.com/mikkelchokolate/Veil/internal/client"
	"github.com/mikkelchokolate/Veil/internal/generatedconfig"
	"github.com/mikkelchokolate/Veil/internal/model"
)

// #1098: once an inbound is credential-managed (any normalized binding row,
// even disabled), zero usable credentials must fail closed — never resurrect
// the legacy inbound fallback password in rendered configs or links.

func newBindingTestState(t *testing.T) (*managementState, *client.Service) {
	t.Helper()
	s := &managementState{}
	s.cipher = newTestCipher(t)
	s.settings = Settings{Domain: "x.example", PanelListen: "127.0.0.1:2096"}
	s.inbounds = []Inbound{{
		Name: "hy2", Protocol: "hysteria2", Transport: "udp", Port: 443,
		Enabled: true, Password: "inbound-fallback-pass",
	}}
	db := openApplyTestDB(t)
	repo := client.NewRepository(db)
	creds := client.NewCredentialStore(db, s.cipher)
	svc := client.NewService(repo, creds)
	s.clientService = svc
	s.clientRepo = repo
	return s, svc
}

func assertNoFallbackRendered(t *testing.T, s *managementState, label string) {
	t.Helper()
	inbounds, err := s.inboundsWithRuntimeCredentialsLocked()
	if err != nil {
		t.Fatalf("%s: resolve runtime credentials: %v", label, err)
	}
	if len(inbounds) != 1 {
		t.Fatalf("%s: inbounds = %+v", label, inbounds)
	}
	if !inbounds[0].HasClientBindings {
		t.Fatalf("%s: HasClientBindings = false — the binding row must still mark the inbound as credential-managed", label)
	}
	if len(inbounds[0].RuntimeCredentials) != 0 {
		t.Fatalf("%s: RuntimeCredentials = %+v, want none", label, inbounds[0].RuntimeCredentials)
	}
	configs, err := s.renderManagementConfigsLocked()
	if err != nil {
		t.Fatalf("%s: render: %v", label, err)
	}
	for name, body := range configs {
		if strings.Contains(body, "inbound-fallback-pass") {
			t.Fatalf("%s: fallback password revived in %s after all normalized credentials were revoked:\n%s", label, name, body)
		}
	}
}

func TestClientBindingsDisableKeepsCredentialManagedAndDropsFallback(t *testing.T) {
	s, svc := newBindingTestState(t)
	view, err := svc.Create(client.Client{Name: "alice", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	b, err := svc.AddBinding(view.ID, "hy2")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SetCredential(b.ID, "password", "client-pass"); err != nil {
		t.Fatal(err)
	}
	inbounds, err := s.inboundsWithRuntimeCredentialsLocked()
	if err != nil {
		t.Fatal(err)
	}
	if !inbounds[0].HasClientBindings || len(inbounds[0].RuntimeCredentials) != 1 {
		t.Fatalf("healthy binding must render its credential: %+v", inbounds[0])
	}
	// Disable the binding — the only credential is gone but the fallback must
	// NOT come back.
	if _, err := svc.SetBindingEnabled(b.ID, false, b.Version); err != nil {
		t.Fatal(err)
	}
	assertNoFallbackRendered(t, s, "binding disabled")
}

func TestClientBindingsExpiredClientKeepsCredentialManaged(t *testing.T) {
	s, svc := newBindingTestState(t)
	past := int64(1)
	view, err := svc.Create(client.Client{Name: "bob", Enabled: true, ExpiresAt: &past})
	if err != nil {
		t.Fatal(err)
	}
	b, err := svc.AddBinding(view.ID, "hy2")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SetCredential(b.ID, "password", "client-pass"); err != nil {
		t.Fatal(err)
	}
	assertNoFallbackRendered(t, s, "client expired")
}

func TestClientBindingsDepletedClientKeepsCredentialManaged(t *testing.T) {
	s, svc := newBindingTestState(t)
	// Depleted is only valid alongside a quota — set both so the store accepts
	// the drained state a real quota-crossing would produce.
	quota := int64(1)
	view, err := svc.Create(client.Client{Name: "carol", Enabled: true, QuotaBytes: &quota, Depleted: true})
	if err != nil {
		t.Fatal(err)
	}
	b, err := svc.AddBinding(view.ID, "hy2")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SetCredential(b.ID, "password", "client-pass"); err != nil {
		t.Fatal(err)
	}
	assertNoFallbackRendered(t, s, "client depleted")
}

// The pinned-render path reads the immutable snapshot, not live SQLite — a
// revoked-binding snapshot must still mark the inbound credential-managed.
func TestPinnedRenderKeepsCredentialManagedWhenBindingDisabled(t *testing.T) {
	s, svc := newBindingTestState(t)
	view, err := svc.Create(client.Client{Name: "alice", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	b, err := svc.AddBinding(view.ID, "hy2")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SetCredential(b.ID, "password", "client-pass"); err != nil {
		t.Fatal(err)
	}
	snap := mustStateSnapshot(t, s)
	applyRenderSnapshot(s, snap)
	// Disable the binding in LIVE state after pinning — the pinned snapshot
	// already carries the disabled row, so the marker must come from it.
	if _, err := svc.SetBindingEnabled(b.ID, false, b.Version); err != nil {
		t.Fatal(err)
	}
	// Force a fresh snapshot WITH the disabled row, pin to it.
	snap = mustStateSnapshot(t, s)
	applyRenderSnapshot(s, snap)
	inbounds, err := s.inboundsWithRuntimeCredentialsLocked()
	if err != nil {
		t.Fatal(err)
	}
	if !inbounds[0].HasClientBindings {
		t.Fatal("pinned render lost the credential-managed marker for a disabled binding")
	}
	if len(inbounds[0].RuntimeCredentials) != 0 {
		t.Fatalf("pinned render carried a credential for a disabled binding: %+v", inbounds[0].RuntimeCredentials)
	}
	configs, err := s.renderManagementConfigsLocked()
	if err != nil {
		t.Fatal(err)
	}
	for name, body := range configs {
		if strings.Contains(body, "inbound-fallback-pass") {
			t.Fatalf("pinned render revived fallback password in %s:\n%s", name, body)
		}
	}
}

// Deleting the inbound's LAST binding removes the marker — the inbound is no
// longer credential-managed and the legacy fallback legitimately returns.
func TestClientBindingsFallbackReturnsOnlyAfterAllBindingsRemoved(t *testing.T) {
	s, svc := newBindingTestState(t)
	view, err := svc.Create(client.Client{Name: "alice", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	b, err := svc.AddBinding(view.ID, "hy2")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SetCredential(b.ID, "password", "client-pass"); err != nil {
		t.Fatal(err)
	}
	if err := svc.RemoveBinding(b.ID, view.ID); err != nil {
		t.Fatal(err)
	}
	inbounds, err := s.inboundsWithRuntimeCredentialsLocked()
	if err != nil {
		t.Fatal(err)
	}
	if inbounds[0].HasClientBindings {
		t.Fatal("inbound still credential-managed after its last binding was removed")
	}
}

// #1098 hardening: when every credential field is empty, the rendered
// sentinel must still be unguessable — keyed by the per-install derivation
// secret the state layer injects, not publicly computable from the inbound
// name alone.
func TestRevokedSentinelKeyedByInstallSecret(t *testing.T) {
	s, svc := newBindingTestState(t)
	// Strip all credential material so only the injected secret can key the
	// sentinel — the exact hole CodeQL/#1098 flagged.
	s.inbounds[0].Password = ""
	view, err := svc.Create(client.Client{Name: "alice", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	b, err := svc.AddBinding(view.ID, "hy2")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SetCredential(b.ID, "password", "client-pass"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SetBindingEnabled(b.ID, false, b.Version); err != nil {
		t.Fatal(err)
	}
	// SnapshotLocked back-fills live settings with the derived per-install
	// secret, matching what Store.decryptSnapshot injects on every load.
	snap, err := s.snapshotLocked()
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	secret := snap.Settings.CredentialDerivationSecret
	if secret == "" {
		t.Fatal("snapshot settings did not carry the credential derivation secret")
	}
	inbounds, err := s.inboundsWithRuntimeCredentialsLocked()
	if err != nil {
		t.Fatalf("resolve runtime credentials: %v", err)
	}
	body, err := generatedconfig.NewInboundRenderer(
		snap.Settings,
		generatedconfig.NewPathsWithLiveRoot(s.applyRoot, s.liveRoot),
		s.warp,
	).RenderHysteria2(inbounds[0])
	if err != nil {
		t.Fatalf("render hy2: %v", err)
	}
	_, wantPass := model.RevokedClientCredential(
		model.Settings{CredentialDerivationSecret: secret},
		model.Inbound{Name: "hy2"},
	)
	if !strings.Contains(body, "veil-revoked-hy2") {
		t.Fatalf("rendered config missing the revoked sentinel account:\n%s", body)
	}
	if !strings.Contains(body, wantPass) {
		t.Fatalf("sentinel password not keyed by the install secret:\n%s", body)
	}
}

var _ = model.Inbound{}
