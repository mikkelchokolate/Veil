package api

import "errors"

type ClientLinksResponseFinalizer struct{}

func NewClientLinksResponseFinalizer() ClientLinksResponseFinalizer {
	return ClientLinksResponseFinalizer{}
}

func (ClientLinksResponseFinalizer) Finalize(response ClientLinksResponse) (ClientLinksResponse, error) {
	if len(response.Links) == 0 {
		return ClientLinksResponse{}, errors.New("no enabled client links are available")
	}
	response.Count = len(response.Links)
	return response, nil
}
