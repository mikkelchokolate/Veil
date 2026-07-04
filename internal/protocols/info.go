package protocols

import "github.com/mikkelchokolate/Veil/internal/protocols/schema"

// ProtocolInfo exposes a protocol's metadata and dynamic UI schemas. It is the
// response shape for the /api/protocols endpoint.
type ProtocolInfo struct {
	Metadata
	InboundFieldSchema  []schema.FieldSchema `json:"inboundFieldSchema"`
	SettingsFieldSchema []schema.FieldSchema `json:"settingsFieldSchema"`
}

// ProtocolInfos returns metadata + UI schemas for every registered plugin.
func (r *Registry) ProtocolInfos() []ProtocolInfo {
	out := make([]ProtocolInfo, 0, len(r.order))
	for _, protocol := range r.order {
		plugin := r.byProtocol[protocol]
		info := ProtocolInfo{Metadata: MetadataOf(plugin)}
		if ui, ok := AsUIProvider(plugin); ok {
			info.InboundFieldSchema = ui.InboundFieldSchema()
			info.SettingsFieldSchema = ui.SettingsFieldSchema()
		}
		out = append(out, info)
	}
	return out
}
