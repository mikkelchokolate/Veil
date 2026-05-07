package api

import "strings"

type ClientSubscriptionPayload struct {
	response ClientLinksResponse
}

func NewClientSubscriptionPayload(response ClientLinksResponse) ClientSubscriptionPayload {
	return ClientSubscriptionPayload{response: response}
}

func (p ClientSubscriptionPayload) Build() string {
	uris := make([]string, 0, len(p.response.Links))
	for _, link := range p.response.Links {
		if link.URI == "" {
			continue
		}
		uris = append(uris, link.URI)
	}
	return strings.Join(uris, "\n") + "\n"
}
