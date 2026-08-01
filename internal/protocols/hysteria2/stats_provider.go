package hysteria2

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/mikkelchokolate/Veil/internal/client"
)

const maxTrafficStatsResponseBytes int64 = 1 << 20

type StatsProvider struct {
	key        string
	endpoint   string
	secret     string
	bindings   map[string]string
	httpClient *http.Client
}

func NewStatsProvider(key, endpoint string, bindings map[string]string) *StatsProvider {
	if parsed, err := url.Parse(endpoint); err == nil && (parsed.Path == "" || parsed.Path == "/") {
		parsed.Path = "/traffic"
		endpoint = parsed.String()
	}
	copyBindings := make(map[string]string, len(bindings))
	for runtimeIdentity, bindingID := range bindings {
		copyBindings[runtimeIdentity] = bindingID
	}
	return &StatsProvider{
		key:        key,
		endpoint:   endpoint,
		bindings:   copyBindings,
		httpClient: &http.Client{Timeout: 5 * time.Second},
	}
}

func NewAuthenticatedStatsProvider(key, endpoint, secret string, bindings map[string]string) *StatsProvider {
	provider := NewStatsProvider(key, endpoint, bindings)
	provider.secret = secret
	return provider
}

func (p *StatsProvider) Key() string { return p.key }

type trafficStats struct {
	Tx uint64 `json:"tx"`
	Rx uint64 `json:"rx"`
}

func (p *StatsProvider) Read() (map[string]client.ProviderReading, error) {
	return p.ReadContext(context.Background())
}

func (p *StatsProvider) ReadContext(ctx context.Context) (map[string]client.ProviderReading, error) {
	if p == nil || strings.TrimSpace(p.endpoint) == "" {
		return nil, errors.New("hysteria2 traffic endpoint is not configured")
	}
	parsed, err := url.Parse(p.endpoint)
	if err != nil || parsed.Scheme != "http" || parsed.Hostname() == "" || parsed.User != nil {
		return nil, errors.New("hysteria2 traffic endpoint must be an absolute HTTP URL")
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, p.endpoint, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Authorization", p.secret)
	httpClient := p.httpClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 5 * time.Second}
	}
	response, err := httpClient.Do(request)
	if err != nil {
		return nil, fmt.Errorf("hysteria2 traffic request: %w", err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, maxTrafficStatsResponseBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read hysteria2 traffic response: %w", err)
	}
	if int64(len(body)) > maxTrafficStatsResponseBytes {
		return nil, fmt.Errorf("hysteria2 traffic response is too large")
	}
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("hysteria2 traffic status %d", response.StatusCode)
	}
	var payload map[string]trafficStats
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("decode hysteria2 traffic response: %w", err)
	}
	out := make(map[string]client.ProviderReading, len(payload))
	var unknown []string
	for runtimeIdentity, counters := range payload {
		bindingID, ok := p.bindings[runtimeIdentity]
		if !ok || bindingID == "" {
			unknown = append(unknown, runtimeIdentity)
			continue
		}
		if counters.Tx > math.MaxInt64 || counters.Rx > math.MaxInt64 {
			return nil, fmt.Errorf("hysteria2 traffic counter for identity %q exceeds int64 range", runtimeIdentity)
		}
		out[bindingID] = client.ProviderReading{
			BindingID:     bindingID,
			UploadBytes:   int64(counters.Tx),
			DownloadBytes: int64(counters.Rx),
		}
	}
	if len(unknown) > 0 {
		return nil, fmt.Errorf("hysteria2 traffic response contains %d unknown runtime identities", len(unknown))
	}
	return out, nil
}
