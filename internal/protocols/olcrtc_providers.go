package protocols

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
)

// OlcrtcProvider is a "meet" service olcRTC disguises its tunnel as.
type OlcrtcProvider struct {
	Name string
	// AutoRoom reports whether a room can be generated automatically. Jitsi
	// rooms are created on join, so any random room name works and the operator
	// needs no manual setup. Telemost and WbStream require a room to be created
	// on the service's website first, so a room cannot be auto-generated.
	AutoRoom bool
}

// OlcrtcProviders is the authoritative list of olcRTC meet providers and
// whether each supports automatic room generation. The panel renders the
// "Generate" room button as enabled only for providers with AutoRoom.
func OlcrtcProviders() []OlcrtcProvider {
	return []OlcrtcProvider{
		{Name: "jitsi", AutoRoom: true},
		{Name: "telemost", AutoRoom: false},
		{Name: "wbstream", AutoRoom: false},
	}
}

// OlcrtcProviderSupportsAutoRoom reports whether the named provider can have a
// room auto-generated. Unknown providers are treated as non-auto.
func OlcrtcProviderSupportsAutoRoom(name string) bool {
	for _, p := range OlcrtcProviders() {
		if p.Name == name {
			return p.AutoRoom
		}
	}
	return false
}

// olcrtcJitsiRoomBase is the community Jitsi instance olcRTC tunnels through.
// A random per-room suffix is appended; both ends share the resulting URL.
const olcrtcJitsiRoomBase = "https://meet.small-dm.ru/veil-"

// GenerateOlcrtcRoom returns a fresh room id for a provider that supports
// automatic room creation, or an error for a provider that requires a manually
// created room (so callers — the API and inbound auto-fill — refuse rather
// than emit a broken config).
func GenerateOlcrtcRoom(provider string) (string, error) {
	if !OlcrtcProviderSupportsAutoRoom(provider) {
		return "", fmt.Errorf("olcRTC provider %q requires a room created on the service first", provider)
	}
	b := make([]byte, 6)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return olcrtcJitsiRoomBase + hex.EncodeToString(b), nil
}
