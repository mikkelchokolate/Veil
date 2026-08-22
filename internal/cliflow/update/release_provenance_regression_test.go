package update

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReleaseAssetsRejectUnsignedOrIncorrectlySignedChecksumsAndProvenance(t *testing.T) {
	assetName := AssetName()
	archive := []byte("archive-body")
	digest := sha256.Sum256(archive)
	checksums := []byte(fmt.Sprintf("%s  %s\n", hex.EncodeToString(digest[:]), assetName))

	cases := []struct {
		name  string
		extra map[string][]byte
	}{
		{name: "unsigned"},
		{name: "malformed bundle", extra: map[string][]byte{"checksums.txt.sigstore.json": []byte(`{"not":"a bundle"}`), "checksums.txt.intoto.jsonl": []byte(`{}`)}},
		{name: "wrong oidc issuer", extra: map[string][]byte{"checksums.txt.sigstore.json": []byte(`{"issuer":"https://attacker.invalid","repository":"mikkelchokolate/Veil","workflow":"release.yml","ref":"refs/tags/v1.2.4"}`), "checksums.txt.intoto.jsonl": []byte(`{"subject":[]}`)}},
		{name: "wrong repository identity", extra: map[string][]byte{"checksums.txt.sigstore.json": []byte(`{"issuer":"https://token.actions.githubusercontent.com","repository":"attacker/Veil","workflow":"release.yml","ref":"refs/tags/v1.2.4"}`), "checksums.txt.intoto.jsonl": []byte(`{"subject":[]}`)}},
		{name: "wrong workflow", extra: map[string][]byte{"checksums.txt.sigstore.json": []byte(`{"issuer":"https://token.actions.githubusercontent.com","repository":"mikkelchokolate/Veil","workflow":"untrusted.yml","ref":"refs/tags/v1.2.4"}`), "checksums.txt.intoto.jsonl": []byte(`{"subject":[]}`)}},
		{name: "wrong tag ref", extra: map[string][]byte{"checksums.txt.sigstore.json": []byte(`{"issuer":"https://token.actions.githubusercontent.com","repository":"mikkelchokolate/Veil","workflow":"release.yml","ref":"refs/heads/main"}`), "checksums.txt.intoto.jsonl": []byte(`{"subject":[]}`)}},
		{name: "wrong provenance subject", extra: map[string][]byte{"checksums.txt.sigstore.json": []byte(`{"issuer":"https://token.actions.githubusercontent.com","repository":"mikkelchokolate/Veil","workflow":"release.yml","ref":"refs/tags/v1.2.4"}`), "checksums.txt.intoto.jsonl": []byte(`{"subject":[{"name":"other.tar.gz","digest":{"sha256":"00"}}]}`)}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			bodies := map[string][]byte{
				"https://example/archive":   archive,
				"https://example/checksums": checksums,
			}
			releaseAssets := []Asset{
				{Name: assetName, BrowserDownloadURL: "https://example/archive"},
				{Name: "checksums.txt", BrowserDownloadURL: "https://example/checksums"},
			}
			for name, body := range tc.extra {
				url := "https://example/" + name
				releaseAssets = append(releaseAssets, Asset{Name: name, BrowserDownloadURL: url})
				bodies[url] = body
			}
			assets := NewReleaseAssets(&Release{TagName: "v1.2.4", Assets: releaseAssets}, func(url string) ([]byte, error) {
				body, ok := bodies[url]
				if !ok {
					return nil, fmt.Errorf("unexpected URL %s", url)
				}
				return body, nil
			})
			if _, err := assets.DownloadVerifiedArchive(); err == nil {
				t.Fatal("archive/checksums from the same release were trusted without valid independent signature and SLSA provenance")
			}
		})
	}
}

func TestInstallerRequiresCosignIdentityAndSLSAProvenanceBeforeChecksum(t *testing.T) {
	body, err := os.ReadFile(filepath.Join("..", "..", "..", "scripts", "install.sh"))
	if err != nil {
		t.Fatal(err)
	}
	script := string(body)
	for _, required := range []string{
		"verify-blob --bundle",
		"https://token.actions.githubusercontent.com",
		"mikkelchokolate/Veil",
		"release.yml",
		"refs/tags/",
		"slsa",
		"provenance",
	} {
		if !strings.Contains(script, required) {
			t.Errorf("installer does not enforce release trust requirement %q", required)
		}
	}
	cosignAt := strings.Index(script, `"$cosign" verify-blob`)
	archiveChecksumAt := strings.Index(script, `awk -v asset="$asset" '($2 == asset || $2 == "./" asset)`)
	if cosignAt < 0 || archiveChecksumAt < 0 || cosignAt > archiveChecksumAt {
		t.Error("installer must verify signatures and provenance before trusting the release archive checksum")
	}
}
