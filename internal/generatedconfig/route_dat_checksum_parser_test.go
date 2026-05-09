package generatedconfig

import (
	"crypto/sha256"
	"encoding/hex"
	"testing"
)

func TestRouteDatChecksumParserFindsChecksumByFilenameAndNormalizesPrefix(t *testing.T) {
	sum := sha256.Sum256([]byte("body"))
	checksum := "sha256:" + hex.EncodeToString(sum[:]) + " geosite.dat"
	parsed, err := NewRouteDatChecksumParser().Parse("geosite.dat", checksum)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if parsed != hex.EncodeToString(sum[:]) {
		t.Fatalf("parsed = %q", parsed)
	}
}

func TestRouteDatChecksumParserRejectsEmptyAndInvalidChecksum(t *testing.T) {
	if _, err := NewRouteDatChecksumParser().Parse("geosite.dat", ""); err == nil || err.Error() != "checksum for geosite.dat is empty" {
		t.Fatalf("empty err = %v", err)
	}
	if _, err := NewRouteDatChecksumParser().Parse("geosite.dat", "nothex geosite.dat"); err == nil || err.Error() != "invalid checksum for geosite.dat" {
		t.Fatalf("invalid err = %v", err)
	}
}
