package protocols

import (
	"github.com/mikkelchokolate/Veil/internal/clientaccess"
	"github.com/mikkelchokolate/Veil/internal/model"
)

// ClientAccessAggregator builds client links for protocols that need to see all
// enabled inbounds at once, such as Mieru's combined client config.
type ClientAccessAggregator interface {
	AggregateLinks(settings model.Settings, inbounds []model.Inbound) ([]model.ClientLink, error)
}

// BuildClientLinks creates user-facing client connection links using the
// protocol plugin registry as the source of truth.
func BuildClientLinks(settings model.Settings, inbounds []model.Inbound) (model.ClientLinksResponse, error) {
	if err := clientaccess.NewClientLinksSettingsValidation().Validate(settings); err != nil {
		return model.ClientLinksResponse{}, err
	}
	response := clientaccess.NewClientLinksResponseMetadata(settings).Build()
	links, err := BuildClientAccessLinks(NewRegistry(), settings, inbounds)
	if err != nil {
		return model.ClientLinksResponse{}, err
	}
	response.Links = append(response.Links, links...)
	return clientaccess.NewClientLinksResponseFinalizer().Finalize(response)
}

// BuildClientAccessLinks creates raw client links from a provided registry. It
// is separated from BuildClientLinks so tests can inject mock protocol plugins.
func BuildClientAccessLinks(r *Registry, settings model.Settings, inbounds []model.Inbound) ([]model.ClientLink, error) {
	byProtocol := map[string][]model.Inbound{}
	order := []string{}
	for _, inbound := range inbounds {
		if !inbound.Enabled {
			continue
		}
		p, ok := r.Get(inbound.Protocol)
		if !ok {
			continue
		}
		_, hasProvider := AsClientAccessProvider(p)
		_, hasAggregator := p.(ClientAccessAggregator)
		if !hasProvider && !hasAggregator {
			continue
		}
		if _, ok := byProtocol[inbound.Protocol]; !ok {
			order = append(order, inbound.Protocol)
		}
		byProtocol[inbound.Protocol] = append(byProtocol[inbound.Protocol], inbound)
	}

	links := []model.ClientLink{}
	for _, protocolName := range order {
		p, _ := r.Get(protocolName)
		selected := byProtocol[protocolName]
		if aggregator, ok := p.(ClientAccessAggregator); ok {
			aggregated, err := aggregator.AggregateLinks(settings, selected)
			if err != nil {
				return nil, err
			}
			links = append(links, aggregated...)
			continue
		}
		provider, ok := AsClientAccessProvider(p)
		if !ok {
			continue
		}
		for _, inbound := range selected {
			built, err := provider.BuildLinks(settings, inbound)
			if err != nil {
				return nil, err
			}
			links = append(links, built...)
		}
	}
	return links, nil
}
