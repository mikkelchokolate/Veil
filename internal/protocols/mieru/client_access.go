package mieru

import (
	"github.com/mikkelchokolate/Veil/internal/clientaccess"
	"github.com/mikkelchokolate/Veil/internal/model"
)

// BuildLinks is not used for mieru; use AggregateLinks instead.
func (Plugin) BuildLinks(model.Settings, model.Inbound) ([]model.ClientLink, error) {
	return nil, nil
}

// AggregateLinks builds the aggregated mieru client config across all mieru inbounds.
func (Plugin) AggregateLinks(settings model.Settings, inbounds []model.Inbound) ([]model.ClientLink, error) {
	return clientaccess.NewMieruClientAccessAggregator().Build(settings, inbounds)
}
