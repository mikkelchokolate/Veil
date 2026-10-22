package cli

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"

	updateflow "github.com/mikkelchokolate/Veil/internal/cliflow/update"
	"github.com/mikkelchokolate/Veil/internal/releaseverify"
)

type cliSourceProofTransport struct {
	base   http.RoundTripper
	target *url.URL
}

func (transport cliSourceProofTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	request = request.Clone(request.Context())
	if request.URL.Host == "api.github.com" {
		request.URL.Scheme = transport.target.Scheme
		request.URL.Host = transport.target.Host
	}
	return transport.base.RoundTrip(request)
}

func TestUpdateCommandProvesSourceBuildAndPinsSignedReleaseCommit(t *testing.T) {
	base := "ae5124ee9d0c651f86c9c3bc99c021056fc0b699"
	target := strings.Repeat("b", 40)
	for _, status := range []string{"ahead", "behind", "diverged"} {
		t.Run(status, func(t *testing.T) {
			archive := createTarGz(t, "veil", []byte("test-binary"))
			digest := sha256.Sum256(archive)
			checksums := fmt.Sprintf("%s  %s\n", hex.EncodeToString(digest[:]), updateflow.AssetName())
			var downloaded, verified atomic.Int32
			var server *httptest.Server
			server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/repos/mikkelchokolate/Veil/releases/latest":
					assets := []updateflow.Asset{}
					for _, name := range []string{updateflow.AssetName(), "checksums.txt", "checksums.txt.bundle", "veil.provenance.json", "veil.provenance.json.bundle"} {
						assets = append(assets, updateflow.Asset{Name: name, BrowserDownloadURL: server.URL + "/" + name})
					}
					json.NewEncoder(w).Encode(updateflow.Release{TagName: "v0.7.3", Assets: assets})
				case "/repos/mikkelchokolate/Veil/git/ref/tags/v0.7.3":
					fmt.Fprintf(w, `{"object":{"type":"commit","sha":%q}}`, target)
				case "/repos/mikkelchokolate/Veil/compare/" + base + "..." + target:
					fmt.Fprintf(w, `{"status":%q,"base_commit":{"sha":%q},"merge_base_commit":{"sha":%q}}`, status, base, base)
				case "/" + updateflow.AssetName():
					downloaded.Add(1)
					w.Write(archive)
				case "/checksums.txt":
					downloaded.Add(1)
					fmt.Fprint(w, checksums)
				case "/checksums.txt.bundle", "/veil.provenance.json", "/veil.provenance.json.bundle":
					downloaded.Add(1)
					fmt.Fprint(w, "signed-test-evidence")
				default:
					t.Errorf("unexpected update request: %s", r.URL.Path)
					w.WriteHeader(http.StatusNotFound)
				}
			}))
			defer server.Close()
			client := server.Client()
			serverURL, _ := url.Parse(server.URL)
			client.Transport = cliSourceProofTransport{base: client.Transport, target: serverURL}
			oldClient, oldAssetClient, oldVerifier := updateHTTPClient, updateflow.HTTPClient, updateReleaseEvidenceVerifier
			updateHTTPClient = client
			updateReleaseEvidenceVerifier = func(evidence releaseverify.Evidence) error {
				if evidence.SourceCommit != target {
					t.Errorf("proven commit not bound to signed verification: %q", evidence.SourceCommit)
				}
				verified.Add(1)
				return nil
			}
			t.Cleanup(func() {
				updateHTTPClient, updateflow.HTTPClient, updateReleaseEvidenceVerifier = oldClient, oldAssetClient, oldVerifier
			})
			command := newUpdateCommand("main-ae5124ee9d0c (" + base + ")")
			var out bytes.Buffer
			command.SetOut(&out)
			command.SetErr(&out)
			command.SetArgs([]string{"--dry-run"})
			err := command.Execute()
			if status == "ahead" {
				if err != nil || downloaded.Load() == 0 || verified.Load() != 1 || !strings.Contains(out.String(), "Dry run") {
					t.Fatalf("CLI forward transition: error=%v downloaded=%d verified=%d output=%s", err, downloaded.Load(), verified.Load(), out.String())
				}
			} else if err == nil || downloaded.Load() != 0 || verified.Load() != 0 {
				t.Fatalf("CLI unsafe transition: error=%v downloaded=%d verified=%d", err, downloaded.Load(), verified.Load())
			}
		})
	}
}
