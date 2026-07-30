package hysteria2

import (
	"encoding/hex"

	"github.com/mikkelchokolate/Veil/internal/model"
	"golang.org/x/crypto/argon2"
)

const (
	trafficStatsArgonTime    = 2
	trafficStatsArgonMemory  = 19 * 1024
	trafficStatsArgonThreads = 1
	trafficStatsSecretBytes  = 32
)

func TrafficStatsSecret(settings model.Settings, inbound model.Inbound) string {
	password := []byte(hysteria2Password(settings, inbound))
	salt := []byte("veil-hysteria2-traffic-stats\x00" + inbound.Name)
	digest := argon2.IDKey(
		password,
		salt,
		trafficStatsArgonTime,
		trafficStatsArgonMemory,
		trafficStatsArgonThreads,
		trafficStatsSecretBytes,
	)
	return hex.EncodeToString(digest)
}
