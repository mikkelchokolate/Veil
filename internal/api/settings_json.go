package api

import (
	"bytes"
	"encoding/json"
)

type settingsWire struct {
	PanelListen       string  `json:"panelListen"`
	PanelAccess       string  `json:"panelAccess,omitempty"`
	WebBasePath       string  `json:"webBasePath,omitempty"`
	Stack             *string `json:"stack,omitempty"`
	Mode              string  `json:"mode"`
	Domain            string  `json:"domain,omitempty"`
	Email             string  `json:"email,omitempty"`
	NaiveUsername     string  `json:"naiveUsername,omitempty"`
	NaivePassword     string  `json:"naivePassword,omitempty"`
	Hysteria2Password string  `json:"hysteria2Password,omitempty"`
	MasqueradeURL     string  `json:"masqueradeURL,omitempty"`
	FallbackRoot      string  `json:"fallbackRoot,omitempty"`
}

func (s *Settings) UnmarshalJSON(body []byte) error {
	var wire settingsWire
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&wire); err != nil {
		return err
	}
	legacyStack := ""
	if wire.Stack != nil {
		legacyStack = *wire.Stack
	}
	*s = Settings{
		PanelListen:       wire.PanelListen,
		PanelAccess:       wire.PanelAccess,
		WebBasePath:       wire.WebBasePath,
		legacyStack:       legacyStack,
		Mode:              wire.Mode,
		Domain:            wire.Domain,
		Email:             wire.Email,
		NaiveUsername:     wire.NaiveUsername,
		NaivePassword:     wire.NaivePassword,
		Hysteria2Password: wire.Hysteria2Password,
		MasqueradeURL:     wire.MasqueradeURL,
		FallbackRoot:      wire.FallbackRoot,
	}
	return nil
}
