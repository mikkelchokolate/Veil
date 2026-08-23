package olcrtc

import (
	"fmt"
)

// Provider is a "meet" service olcRTC disguises its tunnel as.
type Provider struct {
	Name string
	// AutoRoom reports whether a room can be generated automatically. Jitsi
	// rooms are created on join, so any random room name works and the operator
	// needs no manual setup. Telemost and WbStream require a room to be created
	// on the service's website first, so a room cannot be auto-generated.
	AutoRoom bool
}

// Providers is the authoritative list of olcRTC meet providers and whether each
// supports automatic room generation. The panel renders the "Generate" room
// button as enabled only for providers with AutoRoom.
func Providers() []Provider {
	return []Provider{
		{Name: "jitsi", AutoRoom: true},
		{Name: "telemost", AutoRoom: false},
		{Name: "wbstream", AutoRoom: false},
	}
}

// ProviderSupportsAutoRoom reports whether the named provider can have a room
// auto-generated. Unknown providers are treated as non-auto.
func ProviderSupportsAutoRoom(name string) bool {
	for _, p := range Providers() {
		if p.Name == name {
			return p.AutoRoom
		}
	}
	return false
}

// JitsiRoomBase is the Jitsi instance used for auto-generated rooms.
// meet.handyweb.org started returning HTTP 468 on /xmpp-websocket (WAF),
// which makes every generated room crash-loop. This host is first in
// olcRTC's published instance list and accepts anonymous MUC joins.
const JitsiRoomBase = "https://meet.egovm.ru/"

// GenerateRoom returns a fresh room id for a provider that supports automatic
// room creation, or an error for a provider that requires a manually created
// room (so callers — the API and inbound auto-fill — refuse rather than emit a
// broken config). The generated URL is indistinguishable from a room a person
// created in the Jitsi UI.
func GenerateRoom(provider string) (string, error) {
	if !ProviderSupportsAutoRoom(provider) {
		return "", fmt.Errorf("olcRTC provider %q requires a room created on the service first", provider)
	}
	name, err := randomRoomName()
	if err != nil {
		return "", err
	}
	return JitsiRoomBase + name, nil
}
