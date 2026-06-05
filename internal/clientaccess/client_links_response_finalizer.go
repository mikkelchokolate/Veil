package clientaccess

type ClientLinksResponseFinalizer struct{}

func NewClientLinksResponseFinalizer() ClientLinksResponseFinalizer {
	return ClientLinksResponseFinalizer{}
}

func (ClientLinksResponseFinalizer) Finalize(response ClientLinksResponse) (ClientLinksResponse, error) {
	response.Count = len(response.Links)
	response.Artifacts = NewClientAccessDelivery(response).Artifacts()
	return response, nil
}
