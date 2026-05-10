package api

import (
	"path/filepath"
	"testing"
)

func TestManagementTransactionPersistsInboundMutation(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "state.json")
	keyPath := filepath.Join(dir, "state.key")
	state := newManagementState(ServerInfo{StatePath: statePath, KeyPath: keyPath})

	err := state.withTransaction(func(tx *managementTransaction) error {
		_, err := tx.Mutation().CreateInbound(Inbound{Name: "extra", Protocol: "naiveproxy", Transport: "tcp", Port: 444, Enabled: true})
		return err
	})
	if err != nil {
		t.Fatalf("withTransaction: %v", err)
	}

	reloaded := newManagementState(ServerInfo{StatePath: statePath, KeyPath: keyPath})
	reloaded.mu.Lock()
	defer reloaded.mu.Unlock()
	if _, ok := (&managementTransaction{state: reloaded}).Mutation().Inbound("extra"); !ok {
		t.Fatalf("created inbound was not persisted: %+v", reloaded.inbounds)
	}
}
