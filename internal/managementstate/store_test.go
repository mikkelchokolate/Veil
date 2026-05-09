package managementstate

import (
	"path/filepath"
	"testing"

	"github.com/veil-panel/veil/internal/model"
)

func TestStoreSavesAndLoadsManagementStateModel(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	store := NewStore(path, nil)
	snapshot := model.ManagementSnapshot{Settings: model.Settings{PanelListen: "127.0.0.1:2096", Mode: "dev"}, Inbounds: []model.Inbound{{Name: "mieru", Protocol: "mieru", Transport: "tcp", Port: 443}}}
	if err := store.Save(snapshot); err != nil {
		t.Fatalf("Save: %v", err)
	}
	loaded, ok, err := store.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !ok || loaded.Settings.PanelListen != snapshot.Settings.PanelListen || len(loaded.Inbounds) != 1 {
		t.Fatalf("loaded = %+v ok=%v", loaded, ok)
	}
}
