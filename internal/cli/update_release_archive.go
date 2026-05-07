package cli

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
)

type UpdateReleaseArchive struct {
	body []byte
}

func NewUpdateReleaseArchive(body []byte) UpdateReleaseArchive {
	return UpdateReleaseArchive{body: body}
}

func (a UpdateReleaseArchive) ExtractVeilBinary() ([]byte, error) {
	gz, err := gzip.NewReader(bytes.NewReader(a.body))
	if err != nil {
		return nil, fmt.Errorf("gzip decompress: %w", err)
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("tar read: %w", err)
		}
		if hdr.Name == "veil" || hdr.Name == "./veil" {
			const maxBinSize = 100 * 1024 * 1024 // 100 MB
			return io.ReadAll(io.LimitReader(tr, maxBinSize))
		}
	}
	return nil, fmt.Errorf("veil binary not found in archive")
}

// extractVeilBinary extracts the "veil" binary from a tar.gz archive.
func extractVeilBinary(archive []byte) ([]byte, error) {
	return NewUpdateReleaseArchive(archive).ExtractVeilBinary()
}
