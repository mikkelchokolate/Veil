package api

import "strings"

type ClientAccessDelivery struct {
	response ClientLinksResponse
}

func NewClientAccessDelivery(response ClientLinksResponse) ClientAccessDelivery {
	return ClientAccessDelivery{response: response}
}

func (d ClientAccessDelivery) Artifacts() []ClientArtifact {
	artifacts := []ClientArtifact{}
	for _, link := range d.response.Links {
		if link.Config == "" {
			continue
		}
		artifacts = append(artifacts, ClientArtifact{
			Name:     link.Name,
			Protocol: link.Protocol,
			Kind:     "client_config",
			Filename: clientArtifactFilename(link),
			Content:  link.Config,
		})
	}
	return artifacts
}

func (d ClientAccessDelivery) SubscriptionPayload() string {
	uris := make([]string, 0, len(d.response.Links))
	for _, link := range d.response.Links {
		if link.URI == "" {
			continue
		}
		uris = append(uris, link.URI)
	}
	return strings.Join(uris, "\n") + "\n"
}

func clientArtifactFilename(link ClientLink) string {
	name := strings.NewReplacer("/", "-", " ", "-", "_", "-").Replace(link.Name)
	if name == "" {
		name = link.Protocol
	}
	return name + ".json"
}
