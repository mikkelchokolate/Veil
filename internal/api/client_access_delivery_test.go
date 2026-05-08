package api

import "testing"

func TestClientAccessDeliveryBuildsDownloadableMieruArtifacts(t *testing.T) {
	response := ClientLinksResponse{Links: []ClientLink{
		{Name: "hy2", Protocol: "hysteria2", URI: "hysteria2://example"},
		{Name: "mieru/alice", Protocol: "mieru", Config: `{"profileName":"mieru/alice"}`},
	}}
	delivery := NewClientAccessDelivery(response)
	artifacts := delivery.Artifacts()
	if len(artifacts) != 1 {
		t.Fatalf("artifacts = %+v", artifacts)
	}
	artifact := artifacts[0]
	if artifact.Name != "mieru/alice" || artifact.Protocol != "mieru" || artifact.Kind != "client_config" || artifact.Content == "" {
		t.Fatalf("artifact = %+v", artifact)
	}
}

func TestClientAccessDeliveryBuildsSubscriptionPayloadFromURILinksOnly(t *testing.T) {
	payload := NewClientAccessDelivery(ClientLinksResponse{Links: []ClientLink{
		{Name: "mieru", Protocol: "mieru", Config: "{}"},
		{Name: "hy2", Protocol: "hysteria2", URI: "hysteria2://example"},
	}}).SubscriptionPayload()
	if payload != "hysteria2://example\n" {
		t.Fatalf("payload = %q", payload)
	}
}
