package api

import (
	"net/http"

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
