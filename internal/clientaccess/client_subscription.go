package clientaccess

import (
	"encoding/base64"
	"fmt"
	"net/url"
	"sort"
)

const clientSubscriptionContentType = "text/plain; charset=utf-8"

type ClientSubscription struct {
	Body        string
	ContentType string
	Filename    string
}

func ValidateClientSubscriptionQuery(query url.Values) error {
	queryKeys := make([]string, 0, len(query))
	for key := range query {
		queryKeys = append(queryKeys, key)
	}
	sort.Strings(queryKeys)
	for _, key := range queryKeys {
		if key != "format" {
			return fmt.Errorf("unsupported subscription query %q", key)
		}
	}
	_, err := NewClientSubscriptionFormatPolicy().Normalize(query.Get("format"))
	return err
}

func BuildClientSubscription(response ClientLinksResponse, format string) (ClientSubscription, error) {
	format, err := NewClientSubscriptionFormatPolicy().Normalize(format)
	if err != nil {
		return ClientSubscription{}, err
	}
	payload := NewClientSubscriptionPayload(response).Build()
	subscription := ClientSubscription{ContentType: clientSubscriptionContentType}
	if format == "raw" {
		subscription.Body = payload
		subscription.Filename = "veil-subscription-raw.txt"
		return subscription, nil
	}
	subscription.Body = base64.StdEncoding.EncodeToString([]byte(payload)) + "\n"
	subscription.Filename = "veil-subscription.txt"
	return subscription, nil
}
