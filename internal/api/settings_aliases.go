package api

import veilsettings "github.com/veil-panel/veil/internal/settings"

type CredentialDisclosure = veilsettings.CredentialDisclosure

func NewCredentialDisclosure() CredentialDisclosure { return veilsettings.NewCredentialDisclosure() }
