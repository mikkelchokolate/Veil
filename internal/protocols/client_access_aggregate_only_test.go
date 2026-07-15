package protocols

import (
	"testing"

	"github.com/mikkelchokolate/Veil/internal/model"
)

type aggregateOnlyPlugin struct {
	*mockPlugin
	calls int
}

func (p *aggregateOnlyPlugin) AggregateLinks(settings model.Settings, inbounds []model.Inbound) ([]model.ClientLink, error) {
	p.calls++
	return []model.ClientLink{{Name: "combined", Protocol: p.protocol}}, nil
}

func TestBuildClientAccessLinksAllowsAggregateOnlyPlugin(t *testing.T) {
	plugin := &aggregateOnlyPlugin{mockPlugin: &mockPlugin{protocol: "combo", transports: []string{"tcp"}}}
	registry := NewRegistryRaw()
	registry.Register(plugin)

	links, err := BuildClientAccessLinks(registry, model.Settings{}, []model.Inbound{{Name: "edge", Protocol: "combo", Transport: "tcp", Port: 443, Enabled: true}})
	if err != nil {
		t.Fatalf("BuildClientAccessLinks: %v", err)
	}
	if plugin.calls != 1 {
		t.Fatalf("aggregate calls = %d, want 1", plugin.calls)
	}
	if len(links) != 1 || links[0].Name != "combined" || links[0].Protocol != "combo" {
		t.Fatalf("links = %+v", links)
	}
}
