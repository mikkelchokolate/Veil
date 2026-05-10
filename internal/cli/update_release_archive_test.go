package cli

import (
	"testing"

	updateflow "github.com/veil-panel/veil/internal/cliflow/update"
)

func TestUpdateReleaseArchiveExtractsVeilBinary(t *testing.T) {
	archive := updateflow.NewReleaseArchive(createTarGz(t, "./veil", []byte("binary-body")))

	binary, err := archive.ExtractVeilBinary()
	if err != nil {
		t.Fatalf("ExtractVeilBinary: %v", err)
	}
	if string(binary) != "binary-body" {
		t.Fatalf("binary = %q", string(binary))
	}
}
