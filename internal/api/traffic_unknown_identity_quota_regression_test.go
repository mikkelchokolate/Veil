package api

import (
	"context"
	"testing"
	"time"

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
	// The claim is "do not pause": a real pending target must run to
	// completion, not just fail with a non-degraded error. Seed the
	// enforcement row exactly the way the reconciler does — bound to the
	// client's next generation — then run the production enforce path; any
	// error means the leftover identity blocked a quota mutation it should
	// never have touched (#842).
	current, err := state.clientRepo.Get(row.ID)
	if err != nil {
		t.Fatalf("reload client: %v", err)
	}
	mutation := client.BindQuotaTarget(current, client.QuotaMutation{ClientID: row.ID, Depleted: true})
	if _, err := state.db.Exec(`INSERT INTO quota_enforcement
		(client_id,target_generation,target_payload_hash,target_depleted,target_period_epoch,state,next_retry_at,last_error,attempts,updated_at)
		VALUES (?,?,?,?,?,'pending',0,'',0,?)`,
		row.ID, mutation.TargetGeneration, mutation.TargetPayloadHash, 1, mutation.TargetPeriodEpoch, time.Now().UTC().Unix()); err != nil {
		t.Fatalf("seed pending quota target: %v", err)
	}
	if err := state.enforceQuotaMutation(mutation); err != nil {
		t.Fatalf("quota enforcement did not run cleanly for leftover identity: %v", err)
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
