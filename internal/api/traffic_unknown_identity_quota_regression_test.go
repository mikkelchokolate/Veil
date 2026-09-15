package api

import (
	"context"
	"strings"
	"testing"

	"github.com/mikkelchokolate/Veil/internal/client"
)

func TestUnknownIdentitiesDoNotPauseQuotaEnforcement(t *testing.T) {
	state := newClientLifecycleTestState(t)
	row, err := state.clientRepo.Create(client.Client{Name: "quota-unrelated", Enabled: true, QuotaResetPolicy: client.ResetNever})
	if err != nil {
		t.Fatal(err)
	}
	binding, err := state.clientRepo.CreateBinding(client.Binding{ClientID: row.ID, InboundID: "hy-known", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	provider := &unknownIdentityProvider{
		healthRegressionTrafficProvider: healthRegressionTrafficProvider{
			key: "hysteria2:hy-leftover",
			readings: map[string]client.ProviderReading{
				binding.ID: {BindingID: binding.ID, UploadBytes: 10, DownloadBytes: 20},
			},
		},
		unknown: []string{"leftover-user"},
	}
	if err := state.trafficCollector.ResetProductionProviders([]client.TrafficProvider{provider}); err != nil {
		t.Fatal(err)
	}
	if err := state.trafficCollector.CollectOnce(); err != nil {
		t.Fatalf("leftover identity collection: %v", err)
	}
	for _, health := range state.trafficCollector.ProviderHealth() {
		if health.State == "degraded" {
			t.Fatalf("provider degraded by leftover identity: %+v", health)
		}
	}
	err = state.enforceQuotaMutation(client.QuotaMutation{
		ClientID: row.ID, TargetGeneration: 1, TargetPayloadHash: strings.Repeat("a", 64),
	})
	if err != nil && strings.Contains(err.Error(), "degraded") {
		t.Fatalf("quota enforcement paused for leftover identity: %v", err)
	}
}

type unknownIdentityProvider struct {
	healthRegressionTrafficProvider
	unknown []string
}

func (p *unknownIdentityProvider) Read() (client.ProviderBatch, error) {
	batch, err := p.healthRegressionTrafficProvider.Read()
	if err != nil {
		return batch, err
	}
	batch.UnknownIdentities = append([]string(nil), p.unknown...)
	return batch, nil
}

func (p *unknownIdentityProvider) ReadContext(context.Context) (client.ProviderBatch, error) {
	return p.Read()
}
