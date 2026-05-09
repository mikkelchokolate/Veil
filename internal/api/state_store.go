package api

import (
	"github.com/veil-panel/veil/internal/managementstate"
	"github.com/veil-panel/veil/internal/secrets"
)

type StateStore = managementstate.Store

func NewStateStore(path string, cipher *secrets.Cipher) StateStore {
	return managementstate.NewStore(path, cipher)
}
