package update

import "github.com/mikkelchokolate/Veil/internal/releaseverify"

func testReleaseEvidenceAssets() []Asset {
	return []Asset{
		{Name: "checksums.txt.bundle", BrowserDownloadURL: "https://example.com/checksums.bundle"},
		{Name: "veil.provenance.json", BrowserDownloadURL: "https://example.com/provenance"},
		{Name: "veil.provenance.json.bundle", BrowserDownloadURL: "https://example.com/provenance.bundle"},
	}
}

func acceptTestReleaseEvidence(releaseverify.Evidence) error { return nil }

func newTestReleaseAssets(assetName string, downloader func(string) ([]byte, error)) ReleaseAssets {
	assets := append([]Asset{
		{Name: assetName, BrowserDownloadURL: "https://example.com/archive"},
		{Name: "checksums.txt", BrowserDownloadURL: "https://example.com/checksums"},
	}, testReleaseEvidenceAssets()...)
	return NewReleaseAssetsWithVerifier(&Release{TagName: "v1.0.0", Assets: assets}, downloader, acceptTestReleaseEvidence)
}
