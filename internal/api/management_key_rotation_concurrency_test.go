package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mikkelchokolate/Veil/internal/client"
	"github.com/mikkelchokolate/Veil/internal/managementstate"
	"github.com/mikkelchokolate/Veil/internal/privileged"
	"github.com/mikkelchokolate/Veil/internal/secrets"
	"github.com/mikkelchokolate/Veil/internal/statecommit"
)

type blockingRotationClient struct {
	*recordingPrivilegedClient
	statePath string
	keyPath   string
	published chan struct{}
	release   chan struct{}
}

func (c *blockingRotationClient) RotateKey(context.Context, privileged.RotateKeyRequest) error {
	c.rotateCalls++
	_, err := statecommit.RotateKey(statecommit.RotateKeyOptions{
		StatePath: c.statePath, KeyPath: c.keyPath, TargetKeyPath: c.keyPath,
	})
	if err != nil {
		return err
	}
	close(c.published)
	<-c.release
	return nil
}

func TestAPIKeyRotationBlocksSettingsUntilCipherReload(t *testing.T) {
	root := t.TempDir()
	statePath := filepath.Join(root, "state.json")
	keyPath := filepath.Join(root, "state.key")
	privilegedClient := &blockingRotationClient{
		recordingPrivilegedClient: &recordingPrivilegedClient{},
		statePath:                 statePath,
		keyPath:                   keyPath,
		published:                 make(chan struct{}),
		release:                   make(chan struct{}),
	}
	state := newManagementState(ServerInfo{
		Version: "test", Mode: "dev", StatePath: statePath, KeyPath: keyPath,
		ApplyRoot: root, Privileged: privilegedClient,
	})
	defer closeClientSubsystem(state)
	collectorBeforeRotation := state.trafficCollector
	reconcilerBeforeRotation := state.trafficReconciler

	initial := httptest.NewRecorder()
	state.handleSettings(initial, httptest.NewRequest(http.MethodPut, "/api/settings", strings.NewReader(`{
		"panelListen":"127.0.0.1:2096","mode":"dev","domain":"before.example.com",
		"naiveUsername":"veil","naivePassword":"before-secret"
	}`)))
	if initial.Code != http.StatusOK {
		t.Fatalf("initial settings status=%d body=%s", initial.Code, initial.Body.String())
	}
	clientRow, err := state.clientRepo.Create(client.Client{Name: "alice", Enabled: true, QuotaResetPolicy: client.ResetNever})
	if err != nil {
		t.Fatal(err)
	}
	binding, err := state.clientRepo.CreateBinding(client.Binding{ClientID: clientRow.ID, InboundID: "hy2", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	credential, err := state.clientCreds.Set(binding.ID, "password", "normalized-api-secret")
	if err != nil {
		t.Fatal(err)
	}
	oldKey, err := os.ReadFile(keyPath)
	if err != nil {
		t.Fatal(err)
	}

	admin, err := state.sessionRegistry().Create(SessionCreateInput{Username: "admin", Role: "admin"})
	if err != nil {
		t.Fatal(err)
	}
	rotateRequest := adminJSONRequest(http.MethodPost, "/api/admin/rotate-key", `{}`)
	rotateRequest.AddCookie(&http.Cookie{Name: "veil_session", Value: admin.Token})
	rotateResponse := httptest.NewRecorder()
	rotateDone := make(chan struct{})
	go func() {
		defer close(rotateDone)
		state.handleRotateKey(rotateResponse, rotateRequest)
	}()

	select {
	case <-privilegedClient.published:
	case <-time.After(5 * time.Second):
		t.Fatal("rotation did not publish the new key/state pair")
	}

	settingsResponse := httptest.NewRecorder()
	settingsDone := make(chan struct{})
	go func() {
		defer close(settingsDone)
		state.handleSettings(settingsResponse, httptest.NewRequest(http.MethodPut, "/api/settings", strings.NewReader(`{
			"panelListen":"127.0.0.1:2096","mode":"dev","domain":"after.example.com",
			"naiveUsername":"veil","naivePassword":"after-secret"
		}`)))
	}()

	select {
	case <-settingsDone:
		close(privilegedClient.release)
		<-rotateDone
		t.Fatalf("settings mutation completed before rotate-key reload boundary: status=%d body=%s", settingsResponse.Code, settingsResponse.Body.String())
	case <-time.After(150 * time.Millisecond):
		// Expected: handleRotateKey owns s.mu through helper return and reload.
	}
	close(privilegedClient.release)
	select {
	case <-rotateDone:
	case <-time.After(5 * time.Second):
		t.Fatal("rotate-key did not finish after helper release")
	}
	select {
	case <-settingsDone:
	case <-time.After(5 * time.Second):
		t.Fatal("settings mutation remained blocked after rotation reload")
	}
	if rotateResponse.Code != http.StatusOK {
		t.Fatalf("rotate status=%d body=%s", rotateResponse.Code, rotateResponse.Body.String())
	}
	if settingsResponse.Code != http.StatusOK {
		t.Fatalf("settings status=%d body=%s", settingsResponse.Code, settingsResponse.Body.String())
	}
	if state.trafficCollector != collectorBeforeRotation {
		t.Fatal("key rotation leaked the old collector and created a replacement")
	}
	if state.trafficReconciler != reconcilerBeforeRotation {
		t.Fatal("key rotation leaked the old reconciler and created a replacement")
	}

	newKey, err := os.ReadFile(keyPath)
	if err != nil {
		t.Fatal(err)
	}
	newCipher := cipherFromKeyBytesForAPI(t, newKey)
	snapshot, ok, err := managementstate.NewStore(statePath, newCipher).Load()
	if err != nil || !ok {
		t.Fatalf("load state with new key: ok=%v err=%v", ok, err)
	}
	if snapshot.Settings.Domain != "after.example.com" {
		t.Fatalf("final domain=%q", snapshot.Settings.Domain)
	}
	revealed, err := state.clientCreds.Reveal(credential.ID)
	if err != nil || revealed != "normalized-api-secret" {
		t.Fatalf("Panel retained old normalized-client cipher: revealed=%q err=%v", revealed, err)
	}
	oldCipher := cipherFromKeyBytesForAPI(t, oldKey)
	if _, ok, err := managementstate.NewStore(statePath, oldCipher).Load(); err == nil && ok {
		t.Fatal("final settings mutation was encrypted with the pre-rotation cipher")
	}
}

func cipherFromKeyBytesForAPI(t *testing.T, body []byte) *secrets.Cipher {
	t.Helper()
	if len(body) != secrets.KeySize {
		t.Fatalf("key length=%d", len(body))
	}
	var key [secrets.KeySize]byte
	copy(key[:], body)
	cipher, err := secrets.NewCipher(key)
	if err != nil {
		t.Fatal(err)
	}
	return cipher
}
