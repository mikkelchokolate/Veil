package api

import (
	"database/sql"
	"errors"
	"net/http"
	"path/filepath"
	"sync/atomic"
	"testing"

	"golang.org/x/crypto/bcrypt"

	"github.com/mikkelchokolate/Veil/internal/testutil/testdb"
)

func newTestRouter(info ServerInfo) (http.Handler, Reloader) {
	if info.PasswordHasher == nil {
		info.PasswordHasher = bcryptPasswordHasher{cost: bcrypt.MinCost}
	}
	if info.DatabaseOpener == nil {
		info.DatabaseOpener = testdb.CloneIfMissing
	}
	return NewRouter(info)
}

func TestNewManagementStateCarriesDatabaseOpener(t *testing.T) {
	root := t.TempDir()
	statePath := filepath.Join(root, "state.json")
	var calls atomic.Int32
	marker := errors.New("injected opener failure")
	opener := func(string) (*sql.DB, error) {
		calls.Add(1)
		return nil, marker
	}
	state := newManagementState(ServerInfo{StatePath: statePath, DatabaseOpener: opener})
	if state.databaseOpener == nil {
		t.Fatal("database opener was not carried into management state")
	}
	if calls.Load() != 1 {
		t.Fatalf("database opener calls=%d, want 1 for initial lifecycle recovery", calls.Load())
	}
	initApplySubsystem(state)
	if calls.Load() != 2 {
		t.Fatalf("database opener calls=%d, want 2 after explicit reinitialization", calls.Load())
	}
	if !errors.Is(state.storageDegradedErr, marker) {
		t.Fatalf("storage degraded error=%v, want %v", state.storageDegradedErr, marker)
	}
}

// newManagementState is the package-test constructor. It preserves fresh
// storage.Open behavior when a fixture already created veil.db, while ordinary
// lifecycle tests get an isolated prepared clone and a MinCost hasher.
func newManagementState(info ServerInfo) *managementState {
	if info.PasswordHasher == nil {
		info.PasswordHasher = bcryptPasswordHasher{cost: bcrypt.MinCost}
	}
	if info.DatabaseOpener == nil {
		info.DatabaseOpener = testdb.CloneIfMissing
	}
	return newManagementStateProduction(info)
}
