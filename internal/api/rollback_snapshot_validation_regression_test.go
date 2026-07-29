package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"os"
	"reflect"
	"testing"

	"github.com/mikkelchokolate/Veil/internal/model"
)

func TestRollbackRejectsInvalidRuntimeIdentityAndOwnershipSnapshots(t *testing.T) {
	mutations := []struct {
		name   string
		mutate func(*managementSnapshot)
	}{
		{name: "empty_runtime_identity", mutate: func(snapshot *managementSnapshot) {
			snapshot.Bindings[0].RuntimeIdentity = ""
		}},
		{name: "malformed_runtime_identity", mutate: func(snapshot *managementSnapshot) {
			snapshot.Bindings[0].RuntimeIdentity = "invalid identity!"
		}},
		{name: "duplicate_identity_per_inbound", mutate: func(snapshot *managementSnapshot) {
			clientCopy := snapshot.Clients[0]
			clientCopy.ID = "duplicate-client-id"
			clientCopy.Name = "duplicate-client-name"
			snapshot.Clients = append(snapshot.Clients, clientCopy)
			bindingCopy := snapshot.Bindings[0]
			bindingCopy.ID = "duplicate-binding-id"
			bindingCopy.ClientID = clientCopy.ID
			snapshot.Bindings = append(snapshot.Bindings, bindingCopy)
			credentialCopy := snapshot.Credentials[0]
			credentialCopy.ID = "duplicate-credential-id"
			credentialCopy.BindingID = bindingCopy.ID
			snapshot.Credentials = append(snapshot.Credentials, credentialCopy)
		}},
		{name: "binding_without_client_owner", mutate: func(snapshot *managementSnapshot) {
			snapshot.Bindings[0].ClientID = "missing-client-owner"
		}},
		{name: "credential_without_binding_owner", mutate: func(snapshot *managementSnapshot) {
			snapshot.Credentials[0].BindingID = "missing-binding-owner"
		}},
	}

	for _, mutation := range mutations {
		t.Run(mutation.name, func(t *testing.T) {
			router, state := newApplyTrackedRouterWithState(t)
			t.Cleanup(func() { _ = state.Close() })
			inbound := v1Request(t, router, http.MethodPost, "/api/inbounds",
				`{"name":"rollback-validation-hy","protocol":"hysteria2","transport":"udp","port":28443,"enabled":true}`)
			if inbound.Code != http.StatusCreated && inbound.Code != http.StatusOK {
				t.Fatalf("create inbound: %d %s", inbound.Code, inbound.Body.String())
			}
			created := v1Request(t, router, http.MethodPost, "/api/v1/clients",
				`{"name":"rollback-validation-client","bindings":[{"inboundId":"rollback-validation-hy","runtimeIdentity":"valid_runtime_identity","credential":"credential"}]}`)
			if created.Code != http.StatusCreated {
				t.Fatalf("create client: %d %s", created.Code, created.Body.String())
			}
			validRevision, err := state.applyRevisions.Get()
			if err != nil {
				t.Fatal(err)
			}
			validPayload, err := state.applySnapshots.Load(validRevision.Desired)
			if err != nil {
				t.Fatal(err)
			}
			var invalid managementSnapshot
			if err := json.Unmarshal(validPayload, &invalid); err != nil {
				t.Fatal(err)
			}
			if err := state.decryptSnapshot(&invalid); err != nil {
				t.Fatal(err)
			}
			mutation.mutate(&invalid)
			// Re-encrypt credential material exactly as normal snapshot persistence does.
			if err := state.encryptSnapshot(&invalid); err != nil {
				t.Fatal(err)
			}
			invalidPayload, err := json.Marshal(invalid)
			if err != nil {
				t.Fatal(err)
			}
			selectedRevision, err := state.applyRevisions.BumpDesired()
			if err != nil {
				t.Fatal(err)
			}
			if err := state.applySnapshots.Save(selectedRevision, invalidPayload); err != nil {
				t.Fatal(err)
			}
			if _, err := state.applyRevisions.BumpDesired(); err != nil {
				t.Fatal(err)
			}

			stateBefore, err := os.ReadFile(state.statePath)
			if err != nil {
				t.Fatal(err)
			}
			liveBefore := captureSnapshotTestTree(t, state.liveRoot)
			revisionsBefore, err := state.applyRevisions.Get()
			if err != nil {
				t.Fatal(err)
			}
			clientsBefore, err := state.clientRepo.AllClients()
			if err != nil {
				t.Fatal(err)
			}
			bindingsBefore, err := state.clientRepo.AllBindings()
			if err != nil {
				t.Fatal(err)
			}
			jobsBefore := snapshotTestTableCount(t, state, "apply_jobs")

			response := v1Request(t, router, http.MethodPost, "/api/apply/rollback",
				jsonBody(t, map[string]any{"selectedRevision": selectedRevision, "confirm": true}))
			if response.Code != http.StatusUnprocessableEntity {
				t.Errorf("invalid snapshot status = %d, want 422: %s", response.Code, response.Body.String())
			}
			stateAfter, err := os.ReadFile(state.statePath)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(stateBefore, stateAfter) {
				t.Error("state.json changed after invalid rollback")
			}
			revisionsAfter, err := state.applyRevisions.Get()
			if err != nil {
				t.Fatal(err)
			}
			if revisionsAfter != revisionsBefore {
				t.Errorf("revisions changed after invalid rollback: before=%+v after=%+v", revisionsBefore, revisionsAfter)
			}
			clientsAfter, _ := state.clientRepo.AllClients()
			bindingsAfter, _ := state.clientRepo.AllBindings()
			if !reflect.DeepEqual(clientsBefore, clientsAfter) || !reflect.DeepEqual(bindingsBefore, bindingsAfter) {
				t.Error("normalized client ownership changed after invalid rollback")
			}
			if got := snapshotTestTableCount(t, state, "apply_jobs"); got != jobsBefore {
				t.Errorf("apply jobs changed after invalid rollback: %d -> %d", jobsBefore, got)
			}
			if liveAfter := captureSnapshotTestTree(t, state.liveRoot); !reflect.DeepEqual(liveBefore, liveAfter) {
				t.Error("live runtime changed after invalid rollback")
			}
		})
	}
}

func jsonBody(t *testing.T, value any) string {
	t.Helper()
	body, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}

var _ model.ManagementSnapshot
