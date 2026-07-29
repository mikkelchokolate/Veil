package hysteria2

import (
	"crypto/sha256"
	"encoding/hex"

	"github.com/mikkelchokolate/Veil/internal/model"
)

func TrafficStatsSecret(settings model.Settings, inbound model.Inbound) string {
	material := "veil-hysteria2-traffic-stats\x00" + inbound.Name + "\x00" + hysteria2Password(settings, inbound)
	digest := sha256.Sum256([]byte(material))
	return hex.EncodeToString(digest[:])
}
