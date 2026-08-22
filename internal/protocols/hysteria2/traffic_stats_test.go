package hysteria2

import (
	"testing"

	"github.com/mikkelchokolate/Veil/internal/model"
)

func TestTrafficStatsSecretUsesStableMemoryHardKDF(t *testing.T) {
	settings := model.Settings{Hysteria2Password: "correct horse battery staple"}
	inbound := model.Inbound{Name: "edge"}

	const want = "0b6a48c6e27fbc89a09a834e42aab20a1df703d2194d8ec3b15ecac45cfabce1"
	if got := TrafficStatsSecret(settings, inbound); got != want {
		t.Fatalf("TrafficStatsSecret() = %q, want Argon2id vector %q", got, want)
	}
}

func TestTrafficStatsSecretIsScopedToInbound(t *testing.T) {
	settings := model.Settings{Hysteria2Password: "same-password"}
	first := TrafficStatsSecret(settings, model.Inbound{Name: "first"})
	second := TrafficStatsSecret(settings, model.Inbound{Name: "second"})
	if first == second {
		t.Fatal("traffic stats secrets must be domain-separated by inbound")
	}
}
