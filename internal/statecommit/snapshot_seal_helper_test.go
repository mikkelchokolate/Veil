package statecommit

import (
	"testing"

	"github.com/mikkelchokolate/Veil/internal/apply"
	"github.com/mikkelchokolate/Veil/internal/secrets"
)

func snapshotSealFor(t *testing.T, cipher *secrets.Cipher) *apply.SnapshotSeal {
	t.Helper()
	seal, err := apply.NewSnapshotSeal(cipher)
	if err != nil {
		t.Fatal(err)
	}
	return seal
}
