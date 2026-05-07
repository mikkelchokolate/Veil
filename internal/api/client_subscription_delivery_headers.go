package api

import (
	"fmt"
	"net/http"
)

type ClientSubscriptionDeliveryHeaders struct {
	subscription ClientSubscription
}

func NewClientSubscriptionDeliveryHeaders(subscription ClientSubscription) ClientSubscriptionDeliveryHeaders {
	return ClientSubscriptionDeliveryHeaders{subscription: subscription}
}

func (h ClientSubscriptionDeliveryHeaders) Apply(header http.Header) {
	NewClientLinkDeliveryHeaders().Apply(header)
	header.Set("Content-Type", h.subscription.ContentType)
	header.Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, h.subscription.Filename))
}
