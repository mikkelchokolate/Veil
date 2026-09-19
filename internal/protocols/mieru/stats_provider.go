package mieru

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"os/exec"
	"sort"
	"strings"
	"time"

	"github.com/mikkelchokolate/Veil/internal/client"
)

const (
	// MitaUDSPath is the appctl Unix socket exposed by veil-mieru.service.
	MitaUDSPath = "/run/veil-mieru/mita.sock"
	// MitaBinaryPath is where the runtime installer places the mita binary.
	MitaBinaryPath        = "/usr/local/bin/mita"
	mieruStatsTimeout     = 5 * time.Second
	maxMitaOutputBytes    = 1 << 20
	maxMitaStderrBytes    = 4 << 10
	mieruMetricsErrorTail = 200
)

// StatsProvider reads per-user cumulative byte counters from the single
// shared mieru daemon via `mita get metrics` (appctl RPC over the local Unix
// socket; never a network endpoint). Runtime identities are the usernames
// rendered into the server config's users table.
type StatsProvider struct {
	key      string
	bindings map[string]string
	mitaPath string
	sockPath string
}

func NewStatsProvider(key string, bindings map[string]string) *StatsProvider {
	copyBindings := make(map[string]string, len(bindings))
	for runtimeIdentity, bindingID := range bindings {
		copyBindings[runtimeIdentity] = bindingID
	}
	return &StatsProvider{key: key, bindings: copyBindings, mitaPath: MitaBinaryPath, sockPath: MitaUDSPath}
}

func (p *StatsProvider) Key() string { return p.key }

func (p *StatsProvider) Read() (client.ProviderBatch, error) {
	return p.ReadContext(context.Background())
}

func (p *StatsProvider) ReadContext(ctx context.Context) (client.ProviderBatch, error) {
	if p == nil || strings.TrimSpace(p.mitaPath) == "" {
		return client.ProviderBatch{}, errors.New("mieru metrics: mita binary is not configured")
	}
	ctx, cancel := context.WithTimeout(ctx, mieruStatsTimeout)
	defer cancel()
	output, err := execGetMetrics(ctx, p.mitaPath, p.sockPath)
	if err != nil {
		return client.ProviderBatch{}, err
	}
	payload, err := parseMetricsOutput(output)
	if err != nil {
		return client.ProviderBatch{}, fmt.Errorf("mieru metrics: %w", err)
	}
	merged := make(map[string]mieruUserMetrics, len(payload.Users))
	var unknown []string
	for runtimeIdentity, counters := range payload.Users {
		if counters.UploadBytes < 0 || counters.DownloadBytes < 0 {
			return client.ProviderBatch{}, fmt.Errorf("mieru metrics: negative counter for identity %q", runtimeIdentity)
		}
		bindingID, ok := p.bindings[runtimeIdentity]
		if !ok || bindingID == "" {
			unknown = append(unknown, runtimeIdentity)
			continue
		}
		prev := merged[bindingID]
		if counters.UploadBytes > math.MaxInt64-prev.UploadBytes || counters.DownloadBytes > math.MaxInt64-prev.DownloadBytes {
			return client.ProviderBatch{}, fmt.Errorf("mieru metrics: counter for identity %q overflows when merged", runtimeIdentity)
		}
		// A migrated client's alias and its v_* identity can both be live;
		// they are separate mieru users, so sum.
		prev.UploadBytes += counters.UploadBytes
		prev.DownloadBytes += counters.DownloadBytes
		merged[bindingID] = prev
	}
	out := make([]client.ProviderReading, 0, len(merged))
	for bindingID, counters := range merged {
		out = append(out, client.ProviderReading{
			BindingID: bindingID, UploadBytes: counters.UploadBytes, DownloadBytes: counters.DownloadBytes,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].BindingID < out[j].BindingID })
	sort.Strings(unknown)
	return client.ProviderBatch{
		Readings: out, UnknownIdentities: unknown, ObservedAt: time.Now().UTC(), RuntimeInstance: p.key,
	}, nil
}

type mieruUserMetrics struct {
	DownloadBytes int64 `json:"DownloadBytes"`
	UploadBytes   int64 `json:"UploadBytes"`
}

type mieruMetricsPayload struct {
	Users map[string]mieruUserMetrics `json:"users"`
}

// parseMetricsOutput decodes the JSON document printed by `mita get metrics`.
// The CLI may emit log lines before the payload, so decoding starts at the
// first JSON object and stops at its end; trailing lines are ignored.
func parseMetricsOutput(output []byte) (*mieruMetricsPayload, error) {
	start := bytes.IndexByte(output, '{')
	if start < 0 {
		return nil, errors.New("mita output contains no metrics JSON")
	}
	var payload mieruMetricsPayload
	if err := json.NewDecoder(bytes.NewReader(output[start:])).Decode(&payload); err != nil {
		return nil, fmt.Errorf("decode mita metrics JSON: %w", err)
	}
	return &payload, nil
}

// execGetMetrics is a seam for tests.
var execGetMetrics = func(ctx context.Context, mitaPath, sockPath string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, mitaPath, "get", "metrics")
	cmd.Env = append(os.Environ(),
		"MITA_UDS_PATH="+sockPath,
		"MITA_INSECURE_UDS=1",
		"MITA_LOG_NO_TIMESTAMP=true",
	)
	var stdout, stderr cappedBuffer
	stdout.limit = maxMitaOutputBytes
	stderr.limit = maxMitaStderrBytes
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if detail := strings.TrimSpace(stderr.String()); detail != "" {
			return nil, fmt.Errorf("mita get metrics: %w: %s", err, tail(detail, mieruMetricsErrorTail))
		}
		return nil, fmt.Errorf("mita get metrics: %w", err)
	}
	if stdout.overflow {
		return nil, errors.New("mita get metrics: output exceeds 1 MiB limit")
	}
	return stdout.Bytes(), nil
}

func tail(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[len(s)-n:]
}

// cappedBuffer bounds process output: excess bytes are dropped and flagged so
// the caller reports an error instead of trusting a truncated payload.
type cappedBuffer struct {
	buf      bytes.Buffer
	limit    int
	overflow bool
}

func (b *cappedBuffer) Write(p []byte) (int, error) {
	if b.buf.Len()+len(p) > b.limit {
		if remaining := b.limit - b.buf.Len(); remaining > 0 {
			b.buf.Write(p[:remaining])
		}
		b.overflow = true
		return len(p), nil
	}
	return b.buf.Write(p)
}

func (b *cappedBuffer) Bytes() []byte  { return b.buf.Bytes() }
func (b *cappedBuffer) String() string { return b.buf.String() }
