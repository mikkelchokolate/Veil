package model

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"
)

// #1098: a revoked sentinel must be unguessable even when every credential
// field is empty — a sentinel derived only from the (public) inbound name
// would let anyone authenticate as veil-revoked-<name> and defeat the
// revocation.

func TestRevokedClientCredentialUnguessableWithEmptyMaterial(t *testing.T) {
	settings := Settings{CredentialDerivationSecret: strings.Repeat("ab", 32)}
	inbound := Inbound{Name: "naive"}
	_, pass := RevokedClientCredential(settings, inbound)
	if pass == "" {
		t.Fatal("empty sentinel password")
	}
	// The publicly computable candidate: sha256 over the label + name with no
	// secret input. If the sentinel equals anything derivable without the
	// per-install secret, revocation is broken.
	publicSum := sha256.Sum256([]byte("veil-naive-revoked\x00naive\x00\x00\x00\x00\x00\x00"))
	if pass == hex.EncodeToString(publicSum[:]) {
		t.Fatal("sentinel password is publicly computable from the inbound name alone")
	}
	emptyKeyMac := hmac.New(sha256.New, nil)
	emptyKeyMac.Write([]byte("veil-revoked-credential\x00naive"))
	if pass == hex.EncodeToString(emptyKeyMac.Sum(nil)) {
		t.Fatal("sentinel password equals HMAC with an empty key — secret was not mixed in")
	}
}

func TestRevokedClientCredentialDeterministicWithSecret(t *testing.T) {
	settings := Settings{CredentialDerivationSecret: "install-secret-material"}
	inbound := Inbound{Name: "hy2"}
	u1, p1 := RevokedClientCredential(settings, inbound)
	u2, p2 := RevokedClientCredential(settings, inbound)
	if u1 != u2 || p1 != p2 {
		t.Fatalf("sentinel not deterministic: (%q,%q) vs (%q,%q)", u1, p1, u2, p2)
	}
	if u1 != "veil-revoked-hy2" {
		t.Fatalf("username = %q", u1)
	}
}

func TestRevokedClientCredentialChangesWithSecret(t *testing.T) {
	inbound := Inbound{Name: "naive"}
	_, p1 := RevokedClientCredential(Settings{CredentialDerivationSecret: "install-a"}, inbound)
	_, p2 := RevokedClientCredential(Settings{CredentialDerivationSecret: "install-b"}, inbound)
	if p1 == p2 {
		t.Fatal("sentinel identical across installs — the per-install secret is not mixed in")
	}
}

func TestRevokedClientCredentialRandomWhenNoSecretOrMaterial(t *testing.T) {
	// Render context with no managed state at all: nothing deterministic can
	// be secret, so the sentinel must fall back to randomness (fail closed)
	// rather than emit a publicly computable password.
	_, p1 := RevokedClientCredential(Settings{}, Inbound{Name: "naive"})
	_, p2 := RevokedClientCredential(Settings{}, Inbound{Name: "naive"})
	if p1 == "" || p2 == "" {
		t.Fatal("empty sentinel password")
	}
	if p1 == p2 {
		t.Fatal("sentinel with no secret material must be random per call, got a fixed value")
	}
}

func TestRevokedClientCredentialDeterministicWithLegacyMaterialOnly(t *testing.T) {
	// Legacy installs render without CredentialDerivationSecret until state is
	// loaded through the store; remaining credential material still provides
	// secret entropy and keeps the derivation deterministic.
	settings := Settings{NaivePassword: "global-secret"}
	inbound := Inbound{Name: "naive"}
	_, p1 := RevokedClientCredential(settings, inbound)
	_, p2 := RevokedClientCredential(settings, inbound)
	if p1 != p2 {
		t.Fatal("sentinel must stay deterministic when credential material exists")
	}
}
