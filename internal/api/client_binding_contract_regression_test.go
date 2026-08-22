package api

import (
	"fmt"
	"net/http"
	"reflect"
	"testing"
)

func TestClientCreateHonorsDisabledBindingAndPatchPreservesBindingMaterial(t *testing.T) {
	router, state := newApplyTrackedRouterWithState(t)
	inbound := postJSON(t, router, "/api/inbounds", `{"name":"contract-hy","protocol":"hysteria2","transport":"udp","port":30443,"enabled":true}`)
	if inbound.Code != http.StatusCreated {
		t.Fatalf("create inbound: %d %s", inbound.Code, inbound.Body.String())
	}
	response := v1Request(t, router, http.MethodPost, "/api/v1/clients", `{"name":"disabled-contract","enabled":false,"bindings":[{"inboundId":"contract-hy","enabled":false,"runtimeIdentity":"stable_contract_identity","credential":"stable-contract-secret"}]}`)
	if response.Code != http.StatusCreated {
		t.Fatalf("create client: %d %s", response.Code, response.Body.String())
	}
	view := unwrapClient(t, response.Body.Bytes())
	clientID, _ := view["id"].(string)
	if enabled, _ := view["enabled"].(bool); enabled {
		t.Error("explicit client enabled=false was not honored")
	}
	bindingObjects, _ := view["bindings"].([]any)
	if len(bindingObjects) != 1 {
		t.Fatalf("bindings=%#v", bindingObjects)
	}
	bindingView, _ := bindingObjects[0].(map[string]any)
	if enabled, _ := bindingView["enabled"].(bool); enabled {
		t.Error("explicit binding enabled=false was not honored")
	}

	beforeClient, err := state.clientRepo.Get(clientID)
	if err != nil {
		t.Fatal(err)
	}
	beforeBindings, err := state.clientRepo.BindingsForClient(clientID)
	if err != nil || len(beforeBindings) != 1 {
		t.Fatalf("bindings: %v %#v", err, beforeBindings)
	}
	beforeCredentials, err := state.clientCreds.ListForBinding(beforeBindings[0].ID)
	if err != nil || len(beforeCredentials) != 1 {
		t.Fatalf("credentials: %v %#v", err, beforeCredentials)
	}
	patch := v1Request(t, router, http.MethodPatch, "/api/v1/clients/"+clientID, fmt.Sprintf(`{"version":%d,"notes":"ordinary edit"}`, beforeClient.Version))
	if patch.Code != http.StatusOK {
		t.Fatalf("patch client: %d %s", patch.Code, patch.Body.String())
	}
	afterBindings, err := state.clientRepo.BindingsForClient(clientID)
	if err != nil {
		t.Fatal(err)
	}
	afterCredentials, err := state.clientCreds.ListForBinding(beforeBindings[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(afterBindings, beforeBindings) {
		t.Errorf("ordinary client PATCH changed binding identity/version/material:\nbefore=%+v\nafter=%+v", beforeBindings, afterBindings)
	}
	if !reflect.DeepEqual(afterCredentials, beforeCredentials) {
		t.Errorf("ordinary client PATCH regenerated/rotated credential:\nbefore=%+v\nafter=%+v", beforeCredentials, afterCredentials)
	}
}

func TestClientPatchRejectsImpossibleQuotaStateAndStaleWritesUniformly(t *testing.T) {
	router, state := newApplyTrackedRouterWithState(t)
	response := v1Request(t, router, http.MethodPost, "/api/v1/clients", `{"name":"quota-contract","quotaBytes":1000,"quotaResetPolicy":"daily","quotaResetAt":2000000000}`)
	if response.Code != http.StatusCreated {
		t.Fatalf("create client: %d %s", response.Code, response.Body.String())
	}
	view := unwrapClient(t, response.Body.Bytes())
	clientID, _ := view["id"].(string)
	created, err := state.clientRepo.Get(clientID)
	if err != nil {
		t.Fatal(err)
	}
	impossible := v1Request(t, router, http.MethodPatch, "/api/v1/clients/"+clientID, fmt.Sprintf(`{"version":%d,"quotaBytes":null}`, created.Version))
	if impossible.Code != http.StatusBadRequest {
		t.Fatalf("quotaBytes=null left daily reset metadata in an impossible composite state: %d %s", impossible.Code, impossible.Body.String())
	}
	unchanged, err := state.clientRepo.Get(clientID)
	if err != nil {
		t.Fatal(err)
	}
	if unchanged.Version != created.Version || unchanged.QuotaBytes == nil || unchanged.QuotaResetPolicy != "daily" || unchanged.QuotaResetAt == nil {
		t.Fatalf("rejected impossible quota PATCH mutated row: before=%+v after=%+v", created, unchanged)
	}
	stale := v1Request(t, router, http.MethodPatch, "/api/v1/clients/"+clientID, fmt.Sprintf(`{"version":%d,"notes":"stale"}`, created.Version-1))
	if stale.Code != http.StatusConflict {
		t.Fatalf("stale client status=%d body=%s", stale.Code, stale.Body.String())
	}
}
