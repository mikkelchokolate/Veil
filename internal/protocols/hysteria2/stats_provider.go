package hysteria2

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/mikkelchokolate/Veil/internal/client"
)

// StatsProvider implements client.TrafficProvider for hysteria2 by reading
// per-user traffic counters from the hysteria2 runtime's stats endpoint or
// stats file. Hysteria2 exposes traffic stats via its HTTP API when configured
// with a stats listener; this provider reads from a stats file written by the
// runtime (or a sidecar collector).
type StatsProvider struct {
	key       string
	statsPath string
	bindings  map[string]string // clientID -> bindingID (for attribution)
	mu        sync.RWMutex
}

// NewStatsProvider creates a hysteria2 traffic provider reading from statsPath.
// bindings maps client usernames to binding IDs for attribution.
func NewStatsProvider(key, statsPath string, bindings map[string]string) *StatsProvider {
	return &StatsProvider{
		key:       key,
		statsPath: statsPath,
		bindings:  bindings,
	}
}

// Key returns the provider key for monotonic runtime state.
func (p *StatsProvider) Key() string { return p.key }

// Read returns absolute upload/download counters per binding ID.
func (p *StatsProvider) Read() (map[string]client.ProviderReading, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	data, err := os.ReadFile(p.statsPath)
	if err != nil {
		if os.IsNotExist(err) {
			// No stats yet — return empty (not an error).
			return map[string]client.ProviderReading{}, nil
		}
		return nil, fmt.Errorf("hysteria2 stats: read %s: %w", p.statsPath, err)
	}

	// Stats file format: JSON array of {user, upload_bytes, download_bytes}.
	var entries []struct {
		User          string `json:"user"`
		UploadBytes   int64  `json:"upload_bytes"`
		DownloadBytes int64  `json:"download_bytes"`
	}
	if err := json.Unmarshal(data, &entries); err != nil {
		return nil, fmt.Errorf("hysteria2 stats: parse %s: %w", p.statsPath, err)
	}

	out := make(map[string]client.ProviderReading, len(entries))
	for _, e := range entries {
		bindingID, ok := p.bindings[e.User]
		if !ok {
			// Unknown user — skip (may be a non-client user or stale entry).
			continue
		}
		out[bindingID] = client.ProviderReading{
			BindingID:     bindingID,
			UploadBytes:   e.UploadBytes,
			DownloadBytes: e.DownloadBytes,
		}
	}
	return out, nil
}

// UpdateBindings refreshes the client->binding mapping (called when clients
// are created/updated).
func (p *StatsProvider) UpdateBindings(bindings map[string]string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.bindings = bindings
}

// StatsFilePath returns the conventional stats file path for a hysteria2
// inbound under the given runtime root.
func StatsFilePath(runtimeRoot, inboundName string) string {
	// Sanitize inbound name for filesystem.
	safe := strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_' {
			return r
		}
		return '_'
	}, inboundName)
	return filepath.Join(runtimeRoot, "hysteria2", safe, "stats.json")
}
