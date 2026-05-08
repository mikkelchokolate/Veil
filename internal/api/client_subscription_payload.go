package api

type ClientSubscriptionPayload struct {
	response ClientLinksResponse
}

func NewClientSubscriptionPayload(response ClientLinksResponse) ClientSubscriptionPayload {
	return ClientSubscriptionPayload{response: response}
}

func (p ClientSubscriptionPayload) Build() string {
	return NewClientAccessDelivery(p.response).SubscriptionPayload()
}
