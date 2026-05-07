package installer

import (
	"fmt"
	"strings"
)

type MieruBinaryArtifact struct {
	URL    string
	Binary BinaryAcquisition
}

type MieruBinaryAcquisition struct{}

func NewMieruBinaryAcquisition() MieruBinaryAcquisition { return MieruBinaryAcquisition{} }

func (MieruBinaryAcquisition) Build(version, osName, arch, sha256 string) (MieruBinaryArtifact, error) {
	if strings.TrimSpace(version) == "" {
		return MieruBinaryArtifact{}, fmt.Errorf("mieru version is required")
	}
	if strings.TrimSpace(osName) == "" {
		return MieruBinaryArtifact{}, fmt.Errorf("mieru os is required")
	}
	normalizedArch, err := NormalizeArch(arch)
	if err != nil {
		return MieruBinaryArtifact{}, err
	}
	assetVersion := strings.TrimPrefix(strings.TrimSpace(version), "v")
	url := fmt.Sprintf("https://github.com/enfein/mieru/releases/download/%s/mieru_%s_%s_%s.tar.gz", strings.TrimSpace(version), assetVersion, osName, normalizedArch)
	return MieruBinaryArtifact{URL: url, Binary: BinaryAcquisition{Name: "mieru", URL: url, Destination: "/usr/local/bin/mieru", SHA256: strings.TrimSpace(sha256)}}, nil
}
