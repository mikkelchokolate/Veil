package api

type ClientLinksResponseMetadata struct {
	settings Settings
}

func NewClientLinksResponseMetadata(settings Settings) ClientLinksResponseMetadata {
	return ClientLinksResponseMetadata{settings: settings}
}

func (m ClientLinksResponseMetadata) Build() ClientLinksResponse {
	return ClientLinksResponse{
		SchemaVersion:              "v1",
		Domain:                     m.settings.Domain,
		Stack:                      normalizedSettingsStack(m.settings.Stack),
		SubscriptionURL:            "/api/client-links/subscription",
		Base64SubscriptionURL:      "/api/client-links/subscription?format=base64",
		RawSubscriptionURL:         "/api/client-links/subscription?format=raw",
		DefaultSubscriptionFormat:  "base64",
		Base64SubscriptionFilename: "veil-subscription.txt",
		RawSubscriptionFilename:    "veil-subscription-raw.txt",
		SubscriptionContentType:    "text/plain; charset=utf-8",
		SubscriptionFormats:        []string{"base64", "raw"},
	}
}
