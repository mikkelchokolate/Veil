package update

import (
	"testing"
)

func TestUpdateReleaseArchiveExtractsVeilBinary(t *testing.T) {
	archive := NewReleaseArchive(createTestTarGz(t, "./veil", []byte("binary-body")))

	binary, err := archive.ExtractVeilBinary()
	if err != nil {
		t.Fatalf("ExtractVeilBinary: %v", err)
	}
	if string(binary) != "binary-body" {
		t.Fatalf("binary = %q", string(binary))
	}
}
