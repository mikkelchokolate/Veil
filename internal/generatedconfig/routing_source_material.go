package generatedconfig

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/mikkelchokolate/Veil/internal/atomicfile"
	"github.com/mikkelchokolate/Veil/internal/releaseverify"
)

var routingSourceNamePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)

type RoutingSourceDownloader func(string) ([]byte, error)
type RoutingSourceContextDownloader func(context.Context, string) ([]byte, error)

type RoutingSourceSignatureVerifier func(context.Context, RoutingSourceFile, []byte, []byte) error

type RoutingSourceMaterial struct {
	applyRoot       string
	source          RoutingSource
	download        RoutingSourceDownloader
	downloadContext RoutingSourceContextDownloader
	context         context.Context
	verifySignature RoutingSourceSignatureVerifier
}

func NewRoutingSourceMaterial(applyRoot string, source RoutingSource) RoutingSourceMaterial {
	return RoutingSourceMaterial{
		applyRoot: applyRoot, source: source, downloadContext: DownloadRouteDatContext, context: context.Background(),
		verifySignature: func(_ context.Context, file RoutingSourceFile, body, bundle []byte) error {
			return releaseverify.VerifyBlob(body, bundle, file.CertificateIdentity, file.CertificateOIDCIssuer)
		},
	}
}

func (m RoutingSourceMaterial) WithDownloader(download RoutingSourceDownloader) RoutingSourceMaterial {
	if download != nil {
		m.download = download
		m.downloadContext = nil
	}
	return m
}

func (m RoutingSourceMaterial) WithContextDownloader(download RoutingSourceContextDownloader) RoutingSourceMaterial {
	if download != nil {
		m.downloadContext = download
		m.download = nil
	}
	return m
}

func (m RoutingSourceMaterial) WithSignatureVerifier(verify RoutingSourceSignatureVerifier) RoutingSourceMaterial {
	if verify != nil {
		m.verifySignature = verify
	}
	return m
}

func (m RoutingSourceMaterial) WithContext(ctx context.Context) RoutingSourceMaterial {
	if ctx != nil {
		m.context = ctx
	}
	return m
}

func (m RoutingSourceMaterial) WriteGenerated() ([]string, error) {
	bodies := make(map[string][]byte, len(m.source.Files))
	for _, file := range m.source.Files {
		if !routingSourceNamePattern.MatchString(file.Name) || path.Base(file.Name) != file.Name {
			return nil, fmt.Errorf("invalid routing source filename %q", file.Name)
		}
		body, err := m.Fetch(file)
		if err != nil {
			return nil, err
		}
		bodies[file.Name] = body
	}
	return m.publishRoutingFiles(bodies)
}

type publishedRoutingFile struct {
	target      string
	backup      string
	hadPrevious bool
	published   bool
}

func (m RoutingSourceMaterial) publishRoutingFiles(bodies map[string][]byte) ([]string, error) {
	rulesRoot := filepath.Join(m.applyRoot, "generated", "rules")
	if err := os.MkdirAll(rulesRoot, 0o755); err != nil {
		return nil, err
	}
	stageRoot, err := os.MkdirTemp(rulesRoot, ".routing-stage-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(stageRoot)
	names := make([]string, 0, len(bodies))
	for name, body := range bodies {
		names = append(names, name)
		if err := atomicfile.Write(filepath.Join(stageRoot, name), body, 0o600, 0o700); err != nil {
			return nil, err
		}
	}
	sort.Strings(names)
	published := make([]publishedRoutingFile, 0, len(names))
	rollback := func(cause error) ([]string, error) {
		for index := len(published) - 1; index >= 0; index-- {
			entry := published[index]
			if entry.published {
				_ = os.Remove(entry.target)
			}
			if entry.hadPrevious {
				if err := os.Rename(entry.backup, entry.target); err != nil {
					cause = errors.Join(cause, fmt.Errorf("restore routing file %s: %w", entry.target, err))
				}
			}
		}
		_ = syncRoutingDirectory(rulesRoot)
		return nil, cause
	}
	written := make([]string, 0, len(names))
	for index, name := range names {
		target := filepath.Join(rulesRoot, name)
		backup := filepath.Join(stageRoot, fmt.Sprintf("backup-%03d-%s", index, name))
		entry := publishedRoutingFile{target: target, backup: backup}
		if info, err := os.Lstat(target); err == nil {
			if !info.Mode().IsRegular() {
				return rollback(fmt.Errorf("existing routing file %s is not regular", name))
			}
			if err := os.Rename(target, backup); err != nil {
				return rollback(err)
			}
			entry.hadPrevious = true
		} else if !errors.Is(err, os.ErrNotExist) {
			return rollback(err)
		}
		published = append(published, entry)
		if err := os.Rename(filepath.Join(stageRoot, name), target); err != nil {
			return rollback(err)
		}
		published[len(published)-1].published = true
		written = append(written, target)
	}
	if err := syncRoutingDirectory(rulesRoot); err != nil {
		return rollback(err)
	}
	return written, nil
}

func syncRoutingDirectory(path string) error {
	directory, err := os.Open(path)
	if err != nil {
		return err
	}
	defer directory.Close()
	return directory.Sync()
}

func (m RoutingSourceMaterial) Fetch(file RoutingSourceFile) ([]byte, error) {
	if file.SHA256URL == "" {
		return nil, fmt.Errorf("routing source %q requires a SHA-256 checksum URL", file.Name)
	}
	hasSignature := file.SignatureURL != "" || file.CertificateIdentity != "" || file.CertificateOIDCIssuer != ""
	completeSignature := file.SignatureURL != "" && file.CertificateIdentity != "" && file.CertificateOIDCIssuer != ""
	if hasSignature && !completeSignature {
		return nil, fmt.Errorf("routing source %q has incomplete authenticated signature metadata", file.Name)
	}
	if !completeSignature && file.PinnedSHA256 == "" {
		return nil, fmt.Errorf("routing source %q requires authenticated signature metadata or a pinned SHA-256 digest", file.Name)
	}
	if file.PinnedSHA256 != "" {
		if len(file.PinnedSHA256) != sha256.Size*2 || strings.ToLower(file.PinnedSHA256) != file.PinnedSHA256 {
			return nil, fmt.Errorf("routing source %q has invalid pinned SHA-256 digest", file.Name)
		}
		if _, err := hex.DecodeString(file.PinnedSHA256); err != nil {
			return nil, fmt.Errorf("routing source %q has invalid pinned SHA-256 digest", file.Name)
		}
	}
	urls := []string{file.URL, file.SHA256URL}
	if completeSignature {
		urls = append(urls, file.SignatureURL)
	}
	for _, rawURL := range urls {
		if err := validateRoutingSourceURL(rawURL); err != nil {
			return nil, err
		}
	}
	ctx := m.context
	if ctx == nil {
		ctx = context.Background()
	}
	download := m.downloadContext
	if m.download != nil {
		download = func(_ context.Context, rawURL string) ([]byte, error) { return m.download(rawURL) }
	}
	if download == nil {
		download = downloadRouteDatContext
	}
	body, err := download(ctx, file.URL)
	if err != nil {
		return nil, err
	}
	const maxRoutingPayload = 64 << 20
	if len(body) > maxRoutingPayload {
		return nil, fmt.Errorf("routing source %q is too large", file.Name)
	}
	checksumBody, err := download(ctx, file.SHA256URL)
	if err != nil {
		return nil, err
	}
	if len(checksumBody) > 64<<10 {
		return nil, fmt.Errorf("routing checksum for %q is too large", file.Name)
	}
	if err := verifyRouteDatChecksum(file.Name, body, string(checksumBody)); err != nil {
		return nil, err
	}
	if file.PinnedSHA256 != "" {
		digest := sha256.Sum256(body)
		if hex.EncodeToString(digest[:]) != file.PinnedSHA256 {
			return nil, fmt.Errorf("routing source %q does not match its pinned SHA-256 digest", file.Name)
		}
	}
	if !completeSignature {
		return body, nil
	}
	bundle, err := download(ctx, file.SignatureURL)
	if err != nil {
		return nil, err
	}
	if len(bundle) > 4<<20 {
		return nil, fmt.Errorf("routing signature bundle for %q is too large", file.Name)
	}
	if m.verifySignature == nil {
		return nil, fmt.Errorf("routing signature verifier is unavailable")
	}
	if err := m.verifySignature(ctx, file, body, bundle); err != nil {
		return nil, fmt.Errorf("verify routing source %q signature: %w", file.Name, err)
	}
	return body, nil
}

func validateRoutingSourceURL(rawURL string) error {
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.Scheme != "https" || parsed.Hostname() == "" || parsed.User != nil {
		return fmt.Errorf("routing source URL must be an absolute HTTPS URL")
	}
	if !routingSourceHostAllowed(parsed.Hostname()) {
		return fmt.Errorf("routing source host is not allowed")
	}
	return nil
}

func routingSourceHostAllowed(host string) bool {
	host = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(host), "."))
	allowed := map[string]struct{}{
		"github.com":                           {},
		"raw.githubusercontent.com":            {},
		"objects.githubusercontent.com":        {},
		"release-assets.githubusercontent.com": {},
		"example.com":                          {},
		"example.test":                         {},
	}
	for _, configured := range strings.Split(os.Getenv("VEIL_ROUTING_ALLOWED_HOSTS"), ",") {
		configured = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(configured), "."))
		if configured != "" && !strings.ContainsAny(configured, "*/") {
			allowed[configured] = struct{}{}
		}
	}
	_, ok := allowed[host]
	return ok
}
