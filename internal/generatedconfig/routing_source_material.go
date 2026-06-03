package generatedconfig

import (
	"path/filepath"

	"github.com/mikkelchokolate/Veil/internal/atomicfile"
)

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
	download := m.download
	if download == nil {
		download = routeDatDownloader
	}
	body, err := download(file.URL)
	if err != nil {
		return nil, err
	}
	if file.SHA256URL == "" {
		return body, nil
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
