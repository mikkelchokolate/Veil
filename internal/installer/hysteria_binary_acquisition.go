package installer

import "strings"

type HysteriaBinaryArtifact struct {
	URL    string
	Binary BinaryAcquisition
}

type HysteriaBinaryAcquisition struct{}

func NewHysteriaBinaryAcquisition() HysteriaBinaryAcquisition { return HysteriaBinaryAcquisition{} }

func (HysteriaBinaryAcquisition) Build(version, osName, arch, sha256 string) (HysteriaBinaryArtifact, error) {
	url, err := Hysteria2ReleaseAssetURL(version, osName, arch)
	if err != nil {
		return HysteriaBinaryArtifact{}, err
	}
	return HysteriaBinaryArtifact{
		URL: url,
		Binary: BinaryAcquisition{
			Name:        "hysteria2",
			URL:         url,
			Destination: "/usr/local/bin/hysteria",
			SHA256:      strings.TrimSpace(sha256),
		},
	}, nil
}
