package generatedconfig

import (
	"fmt"
	"net/url"
	"path"
	"path/filepath"
	"regexp"

	"github.com/mikkelchokolate/Veil/internal/atomicfile"
)

var routingSourceNamePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)

type RoutingSourceDownloader func(string) ([]byte, error)

type RoutingSourceMaterial struct {
	applyRoot string
	source    RoutingSource
	download  RoutingSourceDownloader
}

func NewRoutingSourceMaterial(applyRoot string, source RoutingSource) RoutingSourceMaterial {
	return RoutingSourceMaterial{applyRoot: applyRoot, source: source, download: routeDatDownloader}
}

func (m RoutingSourceMaterial) WithDownloader(download RoutingSourceDownloader) RoutingSourceMaterial {
	if download != nil {
		m.download = download
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
	}
	download := m.download
	if download == nil {
		download = routeDatDownloader
	}
	body, err := download(file.URL)
	if err != nil {
		return nil, err
	}
	checksumBody, err := download(file.SHA256URL)
	if err != nil {
		return nil, err
	}
	if err := verifyRouteDatChecksum(file.Name, body, string(checksumBody)); err != nil {
		return nil, err
	}
	return body, nil
}
