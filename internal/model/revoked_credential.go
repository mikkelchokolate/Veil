package model

import (
	"crypto/sha256"
	"encoding/hex"
)

// RevokedClientCredential returns a deterministic sentinel username/password
// for an inbound whose normalized clients were all revoked (or whose profiles
// are all disabled). Caddy's forward_proxy module treats an EMPTY
// auth_credentials list as "no authentication" — an open relay — and a
// Hysteria2 userpass map must likewise never be empty while the service keeps
// running, so a credential-managed inbound with zero usable credentials still
// renders one account that can never authenticate. The password is a hash over
// the inbound name plus whatever credential material the inbound/settings
// still carry, so it stays stable across renders but is not derivable from
// public state while any configured secret exists (issue #1098).
func RevokedClientCredential(settings Settings, inbound Inbound) (username, password string) {
	material := "veil-naive-revoked\x00" + inbound.Name + "\x00" +
		inbound.Password + "\x00" + inbound.NaivePassword + "\x00" +
		settings.NaivePassword + "\x00" +
		protocolFieldString(inbound.ProtocolFields, "naivePassword") + "\x00" +
		protocolFieldString(inbound.ProtocolFields, "password")
	sum := sha256.Sum256([]byte(material))
	return "veil-revoked-" + inbound.Name, hex.EncodeToString(sum[:])
}

func protocolFieldString(fields map[string]any, key string) string {
	if fields == nil {
		return ""
	}
	v, ok := fields[key]
	if !ok {
		return ""
	}
	s, _ := v.(string)
	return s
}
