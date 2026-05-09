package generatedconfig

import (
	"crypto/sha256"
	"encoding/hex"
	"testing"
)

func TestRouteDatChecksumVerifierAcceptsMatchingChecksumAndRejectsMismatch(t *testing.T) {
	body := []byte("body")
	sum := sha256.Sum256(body)
	checksum := hex.EncodeToString(sum[:]) + " geosite.dat"
	if err := NewRouteDatChecksumVerifier().Verify("geosite.dat", body, checksum); err != nil {
		t.Fatalf("Verify matching: %v", err)
	}
	if err := NewRouteDatChecksumVerifier().Verify("geosite.dat", []byte("other"), checksum); err == nil || err.Error() != "checksum mismatch for geosite.dat" {
		t.Fatalf("mismatch err = %v", err)
	}
}
