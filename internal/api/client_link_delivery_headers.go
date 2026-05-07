package api

import "net/http"

type ClientLinkDeliveryHeaders struct{}

func NewClientLinkDeliveryHeaders() ClientLinkDeliveryHeaders { return ClientLinkDeliveryHeaders{} }

func (ClientLinkDeliveryHeaders) Apply(headers http.Header) {
	headers.Set("Cache-Control", "no-store")
	headers.Set("X-Content-Type-Options", "nosniff")
}
