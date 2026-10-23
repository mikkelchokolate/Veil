package api

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/mikkelchokolate/Veil/internal/client"
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
	// Binding removal is owner-scoped: RemoveBindingTx requires the matching
	// clientID so a swapped pair can never detach the wrong binding (#1225).
	if err := s.clientRepo.WithTx(func(tx *client.Tx) error {
		return svc.RemoveBindingTx(tx, b.ID, "other-client")
	}); !errors.Is(err, client.ErrNotFound) {
		t.Fatalf("mismatched binding delete: %v, want ErrNotFound", err)
	}
	if _, err := s.clientRepo.GetBinding(b.ID); err != nil {
		t.Fatalf("mismatched delete removed the binding anyway: %v", err)
	}
	if err := s.clientRepo.WithTx(func(tx *client.Tx) error {
		return svc.RemoveBindingTx(tx, b.ID, view.ID)
	}); err != nil {
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
//
// The sentinel is only rendered for naiveproxy: an empty forward_proxy
// auth_credentials list would be an open relay, so the Caddy config carries
// one account that can never authenticate. The production hysteria2 plugin
// drops the inbound instead (no artifact, the instance is stopped). This test
// used to render hysteria2 through the dead generatedconfig.InboundRenderer,
// which still emitted a sentinel, so it passed against a code path that no
// production caller used (#1273 item 2). It now drives the real management
// renderer for both protocols.
func TestRevokedSentinelKeyedByInstallSecret(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake caddy probe binary is a POSIX shell script")
	}
	// The naive renderer probes `caddy list-modules` for forward_proxy; a fake
	// binary on PATH keeps the test hermetic on hosts with and without caddy.
	fakeBin := t.TempDir()
	fakeCaddy := filepath.Join(fakeBin, "caddy")
	if err := os.WriteFile(fakeCaddy, []byte("#!/bin/sh\nprintf '%s' '[{\"module_name\":\"http\"},{\"module_name\":\"http.handlers.forward_proxy\"}]'\n"), 0o755); err != nil {
		t.Fatalf("write fake caddy: %v", err)
	}
	t.Setenv("PATH", fakeBin+string(os.PathListSeparator)+os.Getenv("PATH"))

	s, svc := newBindingTestState(t)
	// Strip all credential material so only the injected secret can key the
	// sentinel — the exact hole CodeQL/#1098 flagged.
	s.inbounds[0].Password = ""
	s.inbounds = append(s.inbounds, Inbound{
		Name: "naive", Protocol: "naiveproxy", Transport: "tcp", Port: 443,
		Enabled: true,
	})
	view, err := svc.Create(client.Client{Name: "alice", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	for _, inbound := range []string{"hy2", "naive"} {
		b, err := svc.AddBinding(view.ID, inbound)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := svc.SetCredential(b.ID, "password", "client-pass"); err != nil {
			t.Fatal(err)
		}
		if _, err := svc.SetBindingEnabled(b.ID, false, b.Version); err != nil {
			t.Fatal(err)
		}
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
	s.settings = snap.Settings
	s.settings.DefaultAcmeEmail = "ops@x.example" // the naive Caddy site needs an ACME email
	configs, err := s.renderManagementConfigsLocked()
	if err != nil {
		t.Fatalf("render management configs: %v", err)
	}
	wantUser, wantPass := model.RevokedClientCredential(
		model.Settings{CredentialDerivationSecret: secret},
		model.Inbound{Name: "naive"},
	)
	// The sentinel must actually depend on the install secret: a different
	// secret must derive a different password.
	if _, otherPass := model.RevokedClientCredential(
		model.Settings{CredentialDerivationSecret: secret + "-other"},
		model.Inbound{Name: "naive"},
	); otherPass == wantPass {
		t.Fatal("sentinel password does not depend on the install secret")
	}
	var caddyBody string
	for name, body := range configs {
		if strings.Contains(name, "hysteria2") {
			t.Fatalf("revoked hysteria2 inbound still rendered %s:\n%s", name, body)
		}
		if strings.HasSuffix(name, "caddy/config.json") {
			caddyBody = body
		}
	}
	if caddyBody == "" {
		t.Fatalf("no consolidated Caddy config rendered; rendered: %v", configKeys(configs))
	}
	// The naive listener must authenticate exactly the sentinel: the disabled
	// client's credential must be gone and the list must not be empty (an
	// empty auth_credentials list is an open relay).
	var doc any
	if err := json.Unmarshal([]byte(caddyBody), &doc); err != nil {
		t.Fatalf("parse Caddy JSON: %v", err)
	}
	lists := collectAuthCredentials(doc)
	if len(lists) != 1 {
		t.Fatalf("auth_credentials lists = %v, want exactly one (the naive listener):\n%s", lists, caddyBody)
	}
	want := []string{forwardProxyJSONCredential(wantUser, wantPass)}
	if !reflect.DeepEqual(lists[0], want) {
		t.Fatalf("naive auth_credentials = %v, want only the install-secret-keyed sentinel %s %v", lists[0], wantUser, want)
	}
}

// collectAuthCredentials returns every forward_proxy auth_credentials list in
// a decoded Caddy JSON document.
func collectAuthCredentials(node any) [][]string {
	var out [][]string
	switch v := node.(type) {
	case map[string]any:
		for key, child := range v {
			if key == "auth_credentials" {
				items, _ := child.([]any)
				list := make([]string, 0, len(items))
				for _, item := range items {
					str, _ := item.(string)
					list = append(list, str)
				}
				out = append(out, list)
				continue
			}
			out = append(out, collectAuthCredentials(child)...)
		}
	case []any:
		for _, child := range v {
			out = append(out, collectAuthCredentials(child)...)
		}
	}
	return out
}

func configKeys(configs map[string]string) []string {
	keys := make([]string, 0, len(configs))
	for name := range configs {
		keys = append(keys, name)
	}
	return keys
}

var _ = model.Inbound{}
