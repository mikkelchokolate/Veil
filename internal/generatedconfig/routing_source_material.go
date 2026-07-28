package generatedconfig

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/mikkelchokolate/Veil/internal/atomicfile"
)

var routingSourceNamePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)

type RoutingSourceDownloader func(string) ([]byte, error)
type RoutingSourceContextDownloader func(context.Context, string) ([]byte, error)

type RoutingSourceMaterial struct {
	applyRoot       string
	source          RoutingSource
	download        RoutingSourceDownloader
	downloadContext RoutingSourceContextDownloader
	context         context.Context
}

func NewRoutingSourceMaterial(applyRoot string, source RoutingSource) RoutingSourceMaterial {
	return RoutingSourceMaterial{applyRoot: applyRoot, source: source, downloadContext: DownloadRouteDatContext, context: context.Background()}
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

func (m RoutingSourceMaterial) WithContext(ctx context.Context) RoutingSourceMaterial {
	if ctx != nil {
		m.context = ctx
	}
	return m
}

func (m RoutingSourceMaterial) WriteGenerated() ([]string, error) {
	written := []string{}
	for _, file := range m.source.Files {
		if !routingSourceNamePattern.MatchString(file.Name) || path.Base(file.Name) != file.Name {
			return nil, fmt.Errorf("invalid routing source filename %q", file.Name)
		}
		body, err := m.Fetch(file)
		if err != nil {
			return nil, err
		}
		path := filepath.Join(m.applyRoot, "generated", "rules", file.Name)
		if err := atomicfile.Write(path, body, 0o600, 0o755); err != nil {
			return nil, err
		}
		written = append(written, path)
	}
	return written, nil
}

func (m RoutingSourceMaterial) Fetch(file RoutingSourceFile) ([]byte, error) {
	if file.SHA256URL == "" {
		return nil, fmt.Errorf("routing source %q requires a SHA-256 checksum URL", file.Name)
	}
	for _, rawURL := range []string{file.URL, file.SHA256URL} {
		parsed, err := url.Parse(rawURL)
		if err != nil || parsed.Scheme != "https" || parsed.Hostname() == "" || parsed.User != nil {
			return nil, fmt.Errorf("routing source URL must be an absolute HTTPS URL")
		}
		if !routingSourceHostAllowed(parsed.Hostname()) {
			return nil, fmt.Errorf("routing source host is not allowed")
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
	checksumBody, err := download(ctx, file.SHA256URL)
	if err != nil {
		return nil, err
	}
	if err := verifyRouteDatChecksum(file.Name, body, string(checksumBody)); err != nil {
		return nil, err
	}
	return body, nil
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
