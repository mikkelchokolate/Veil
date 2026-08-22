package api

import (
	"database/sql"
	"encoding/json"
	"strings"
	"testing"

	"github.com/mikkelchokolate/Veil/internal/client"
)

func TestBindingsHaveUniqueStableRuntimeIdentityIndependentOfDisplayName(t *testing.T) {
	_, service, _ := newRuntimeIdentityTestState(t)
	first, err := service.Create(client.Client{Name: "duplicate", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	second, err := service.Create(client.Client{Name: "duplicate", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	firstBinding, err := service.AddBinding(first.ID, "hy2")
	if err != nil {
		t.Fatal(err)
	}
	secondBinding, err := service.AddBinding(second.ID, "hy2")
	if err != nil {
		t.Fatal(err)
	}
	firstIdentity := runtimeIdentityFromJSON(t, firstBinding)
	secondIdentity := runtimeIdentityFromJSON(t, secondBinding)
	if firstIdentity == "" || secondIdentity == "" {
		t.Errorf("binding runtime identities missing: first=%q second=%q", firstIdentity, secondIdentity)
	}
	if firstIdentity != "" && firstIdentity == secondIdentity {
		t.Errorf("duplicate runtime identity on one inbound: %q", firstIdentity)
	}

	updated := first.Client
	updated.Name = "renamed-display-name"
	if _, err := service.Update(updated, first.Version); err != nil {
		t.Fatal(err)
	}
	refreshed, err := service.Get(first.ID)
	if err != nil || len(refreshed.Bindings) != 1 {
		t.Fatalf("bindings after rename: %v %#v", err, refreshed.Bindings)
	}
	if got := runtimeIdentityFromJSON(t, refreshed.Bindings[0]); got != firstIdentity {
		t.Errorf("client rename changed runtime identity: before=%q after=%q", firstIdentity, got)
	}
}

func TestDuplicateDisplayNamesRenderDistinctCredentialsOnSameInbound(t *testing.T) {
	state, service, _ := newRuntimeIdentityTestState(t)
	for index, secret := range []string{"first-secret-unique", "second-secret-unique"} {
		view, err := service.Create(client.Client{Name: "same display", Enabled: true})
		if err != nil {
			t.Fatal(err)
		}
		binding, err := service.AddBinding(view.ID, "hy2")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := service.SetCredential(binding.ID, "password", secret); err != nil {
			t.Fatalf("credential %d: %v", index, err)
		}
	}
	configs, err := state.renderManagementConfigsLocked()
	if err != nil {
		t.Fatal(err)
	}
	joined := joinRendered(configs)
	for _, secret := range []string{"first-secret-unique", "second-secret-unique"} {
		if !strings.Contains(joined, secret) {
			t.Errorf("credential %q omitted/collided in rendered config", secret)
		}
	}
}

func TestClientRenameKeepsRenderedProtocolUsernameStable(t *testing.T) {
	state, service, _ := newRuntimeIdentityTestState(t)
	view, err := service.Create(client.Client{Name: "alice", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	binding, err := service.AddBinding(view.ID, "hy2")
	if err != nil {
		t.Fatal(err)
	}
	const secret = "rename-stability-secret"
	if _, err := service.SetCredential(binding.ID, "password", secret); err != nil {
		t.Fatal(err)
	}
	beforeConfigs, err := state.renderManagementConfigsLocked()
	if err != nil {
		t.Fatal(err)
	}
	before := renderedIdentityForSecret(joinRendered(beforeConfigs), secret)
	updated := view.Client
	updated.Name = "bob"
	if _, err := service.Update(updated, view.Version); err != nil {
		t.Fatal(err)
	}
	afterConfigs, err := state.renderManagementConfigsLocked()
	if err != nil {
		t.Fatal(err)
	}
	after := renderedIdentityForSecret(joinRendered(afterConfigs), secret)
	if before == "" || after == "" || before != after {
		t.Fatalf("rename changed rendered protocol identity: before=%q after=%q", before, after)
	}
}

func TestEnabledBindingMissingOrCorruptCredentialFailsRender(t *testing.T) {
	for _, tc := range []struct {
		name    string
		corrupt bool
	}{
		{name: "missing"},
		{name: "corrupt", corrupt: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			state, service, db := newRuntimeIdentityTestState(t)
			view, err := service.Create(client.Client{Name: "alice", Enabled: true})
			if err != nil {
				t.Fatal(err)
			}
			binding, err := service.AddBinding(view.ID, "hy2")
			if err != nil {
				t.Fatal(err)
			}
			if tc.corrupt {
				if _, err := service.SetCredential(binding.ID, "password", "valid-before-corruption"); err != nil {
					t.Fatal(err)
				}
				if _, err := db.Exec(`UPDATE client_credentials SET encrypted_value=? WHERE binding_id=? AND revoked_at IS NULL`, []byte("corrupt"), binding.ID); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := state.renderManagementConfigsLocked(); err == nil {
				t.Fatal("enabled binding with missing/corrupt credential was silently omitted")
			}
		})
	}
}

func newRuntimeIdentityTestState(t *testing.T) (*managementState, *client.Service, *sql.DB) {
	t.Helper()
	db := openApplyTestDB(t)
	cipher := newTestCipher(t)
	repository := client.NewRepository(db)
	credentials := client.NewCredentialStore(db, cipher)
	service := client.NewService(repository, credentials)
	state := &managementState{
		settings:      Settings{Domain: "x.example", PanelListen: "127.0.0.1:2096"},
		inbounds:      []Inbound{{Name: "hy2", Protocol: "hysteria2", Transport: "udp", Port: 443, Enabled: true, Password: "inbound-pass"}},
		cipher:        cipher,
		clientService: service,
		clientRepo:    repository,
		clientCreds:   credentials,
	}
	return state, service, db
}

func runtimeIdentityFromJSON(t *testing.T, binding any) string {
	t.Helper()
	body, err := json.Marshal(binding)
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(body, &decoded); err != nil {
		t.Fatal(err)
	}
	identity, _ := decoded["runtimeIdentity"].(string)
	return identity
}

func joinRendered(configs map[string]string) string {
	var out strings.Builder
	for _, body := range configs {
		out.WriteString(body)
		out.WriteByte('\n')
	}
	return out.String()
}

func renderedIdentityForSecret(config, secret string) string {
	for _, line := range strings.Split(config, "\n") {
		if !strings.Contains(line, secret) {
			continue
		}
		parts := strings.SplitN(strings.TrimSpace(line), ":", 2)
		if len(parts) == 2 {
			return strings.Trim(parts[0], `"'`)
		}
	}
	return ""
}
