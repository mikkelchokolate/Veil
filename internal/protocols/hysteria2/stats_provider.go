package hysteria2

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/mikkelchokolate/Veil/internal/client"
	"github.com/mikkelchokolate/Veil/internal/runtimeports"
)

const maxTrafficStatsResponseBytes int64 = 1 << 20

type StatsProvider struct {
	key        string
	endpoint   string
	secret     string
	bindings   map[string]string
	httpClient *http.Client
	// instanceSource resolves the serving process's instance identity (set
	// via WithInstanceSource). Queried on every read so a daemon restart is
	// visible at the next poll even when its counters already grew past the
	// pre-restart baseline (#1102).
	instanceSource func(context.Context) string
}

func NewStatsProvider(key, endpoint string, bindings map[string]string) *StatsProvider {
	if parsed, err := url.Parse(endpoint); err == nil && (parsed.Path == "" || parsed.Path == "/") {
		parsed.Path = "/traffic"
		endpoint = parsed.String()
	}
	// Hysteria2 lowercases usernames internally — both the credentials it
	// authenticates and the keys it reports in per-user traffic stats. Fold
	// the identity map so a stored identity still resolves to its binding
	// even when a pre-migration row carries uppercase, and iterate input keys
	// in sorted order so case-only collisions resolve deterministically
	// (#1111).
	copyBindings := make(map[string]string, len(bindings))
	identityKeys := make([]string, 0, len(bindings))
	for runtimeIdentity := range bindings {
		identityKeys = append(identityKeys, runtimeIdentity)
	}
	sort.Strings(identityKeys)
	for _, runtimeIdentity := range identityKeys {
		folded := strings.ToLower(runtimeIdentity)
		if _, exists := copyBindings[folded]; !exists {
			copyBindings[folded] = bindings[runtimeIdentity]
		}
	}
	return &StatsProvider{
		key:        key,
		endpoint:   endpoint,
		bindings:   copyBindings,
		httpClient: &http.Client{Timeout: 5 * time.Second, Transport: statsTransport(endpoint)},
	}
}

// statsTransport binds the dial source to the loopback endpoint address. The
// veil-hysteria2@.service ingress filter (IPAddressAllow) accepts traffic only
// from the 127.40.0.0/16 stats band — binding LocalAddr to the destination
// address makes the panel's source deterministic instead of depending on
// kernel source-address selection for 127/8 (#1095).
func statsTransport(endpoint string) *http.Transport {
	transport := &http.Transport{}
	if parsed, err := url.Parse(endpoint); err == nil {
		if ip := net.ParseIP(parsed.Hostname()); ip != nil && ip.IsLoopback() {
			dialer := &net.Dialer{LocalAddr: &net.TCPAddr{IP: ip}}
			transport.DialContext = dialer.DialContext
		}
	}
	return transport
}

// NewAuthenticatedStatsProvider is the production constructor. The management
// layer historically passes a loopback URL whose port is the inbound's public
// UDP port. Preserve that call contract while translating it to the isolated
// local Traffic Stats endpoint rendered for the same inbound. NewStatsProvider
// remains an exact-endpoint constructor for tests and adapters.
func NewAuthenticatedStatsProvider(key, endpoint, secret string, bindings map[string]string) *StatsProvider {
	endpoint = productionTrafficStatsEndpoint(endpoint)
	provider := NewStatsProvider(key, endpoint, bindings)
	provider.secret = secret
	return provider
}

func productionTrafficStatsEndpoint(endpoint string) string {
	parsed, err := url.Parse(endpoint)
	if err != nil || parsed == nil || parsed.Scheme != "http" || parsed.Hostname() != "127.0.0.1" {
		return endpoint
	}
	port, err := strconv.Atoi(parsed.Port())
	if err != nil || port < 1 || port > 65535 {
		return endpoint
	}
	return runtimeports.Hysteria2TrafficStatsEndpoint(port)
}

// WithInstanceSource wires a lookup that reports the hysteria2 unit's
// current process instance (systemd start timestamp + main PID). A restart
// changes the value, letting the traffic store credit the post-reset counter
// in full instead of silently subtracting the stale baseline (#1102).
func (p *StatsProvider) WithInstanceSource(source func(context.Context) string) *StatsProvider {
	p.instanceSource = source
	return p
}

func (p *StatsProvider) Key() string { return p.key }

type trafficStats struct {
	Tx uint64 `json:"tx"`
	Rx uint64 `json:"rx"`
}

func (p *StatsProvider) Read() (client.ProviderBatch, error) {
	return p.ReadContext(context.Background())
}

func (p *StatsProvider) ReadContext(ctx context.Context) (client.ProviderBatch, error) {
	if p == nil || strings.TrimSpace(p.endpoint) == "" {
		return client.ProviderBatch{}, errors.New("hysteria2 traffic endpoint is not configured")
	}
	parsed, err := url.Parse(p.endpoint)
	loopback := false
	if parsed != nil {
		hostname := parsed.Hostname()
		loopback = strings.EqualFold(hostname, "localhost")
		if address := net.ParseIP(hostname); address != nil {
			loopback = address.IsLoopback()
		}
	}
	if err != nil || parsed.Scheme != "http" || parsed.Hostname() == "" || parsed.User != nil || !loopback {
		return client.ProviderBatch{}, errors.New("hysteria2 traffic endpoint must be an exact loopback HTTP URL")
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, p.endpoint, nil)
	if err != nil {
		return client.ProviderBatch{}, err
	}
	request.Header.Set("Authorization", p.secret)
	httpClient := p.httpClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 5 * time.Second}
	}
	clientCopy := *httpClient
	clientCopy.CheckRedirect = func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }
	response, err := clientCopy.Do(request)
	if err != nil {
		return client.ProviderBatch{}, fmt.Errorf("hysteria2 traffic request: %w", err)
	}
	defer response.Body.Close()
	if response.Request == nil || response.Request.URL == nil || response.Request.URL.String() != parsed.String() {
		return client.ProviderBatch{}, errors.New("hysteria2 traffic response origin changed")
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, maxTrafficStatsResponseBytes+1))
	if err != nil {
		return client.ProviderBatch{}, fmt.Errorf("read hysteria2 traffic response: %w", err)
	}
	if int64(len(body)) > maxTrafficStatsResponseBytes {
		return client.ProviderBatch{}, fmt.Errorf("hysteria2 traffic response is too large")
	}
	if response.StatusCode != http.StatusOK {
		return client.ProviderBatch{}, fmt.Errorf("hysteria2 traffic status %d", response.StatusCode)
	}
	var payload map[string]trafficStats
	if err := json.Unmarshal(body, &payload); err != nil {
		return client.ProviderBatch{}, fmt.Errorf("decode hysteria2 traffic response: %w", err)
	}
	merged := make(map[string]trafficStats, len(payload))
	var unknown []string
	for runtimeIdentity, counters := range payload {
		bindingID, ok := p.bindings[strings.ToLower(runtimeIdentity)]
		if !ok || bindingID == "" {
			unknown = append(unknown, runtimeIdentity)
			continue
		}
		if counters.Tx > math.MaxInt64 || counters.Rx > math.MaxInt64 {
			return client.ProviderBatch{}, fmt.Errorf("hysteria2 traffic counter for identity %q exceeds int64 range", runtimeIdentity)
		}
		prev := merged[bindingID]
		if counters.Tx > math.MaxUint64-prev.Tx || counters.Rx > math.MaxUint64-prev.Rx {
			return client.ProviderBatch{}, fmt.Errorf("hysteria2 traffic counter for identity %q overflows when merged", runtimeIdentity)
		}
		// Leftover pre-migration usernames and v_* identities can both be live
		// for one binding; they are separate Hysteria users, so sum.
		prev.Tx += counters.Tx
		prev.Rx += counters.Rx
		merged[bindingID] = prev
	}
	out := make([]client.ProviderReading, 0, len(merged))
	for bindingID, counters := range merged {
		if counters.Tx > math.MaxInt64 || counters.Rx > math.MaxInt64 {
			return client.ProviderBatch{}, fmt.Errorf("hysteria2 traffic counter for binding %q exceeds int64 range", bindingID)
		}
		out = append(out, client.ProviderReading{
			BindingID: bindingID, UploadBytes: int64(counters.Tx), DownloadBytes: int64(counters.Rx),
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].BindingID < out[j].BindingID })
	sort.Strings(unknown)
	instanceID := ""
	if p.instanceSource != nil {
		instanceID = p.instanceSource(ctx)
	}
	return client.ProviderBatch{
		Readings: out, UnknownIdentities: unknown, ObservedAt: time.Now().UTC(),
		RuntimeInstance: p.key, RuntimeInstanceID: instanceID,
	}, nil
}
