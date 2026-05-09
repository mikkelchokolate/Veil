package generatedconfig

import "testing"

func TestRouteDatBodyLimitAcceptsWithinLimitAndRejectsOversize(t *testing.T) {
	limit := NewRouteDatBodyLimit(3)
	if err := limit.Validate("https://example.com/geosite.dat", []byte{1, 2, 3}); err != nil {
		t.Fatalf("Validate within limit: %v", err)
	}
	err := limit.Validate("https://example.com/geosite.dat", []byte{1, 2, 3, 4})
	if err == nil || err.Error() != "download https://example.com/geosite.dat exceeds maximum size of 3 bytes" {
		t.Fatalf("err = %v", err)
	}
}
