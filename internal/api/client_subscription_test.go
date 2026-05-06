package api

import (
	"encoding/base64"
	"net/url"
	"testing"
)

func TestBuildClientSubscriptionDefaultsToBase64(t *testing.T) {
	subscription, err := BuildClientSubscription(ClientLinksResponse{Links: []ClientLink{{URI: "one"}, {URI: "two"}}}, "")
	if err != nil {
		t.Fatalf("BuildClientSubscription: %v", err)
	}
	if subscription.Filename != "veil-subscription.txt" || subscription.ContentType != "text/plain; charset=utf-8" {
		t.Fatalf("unexpected metadata: %+v", subscription)
	}
	decoded, err := base64.StdEncoding.DecodeString(subscription.Body)
	if err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if string(decoded) != "one\ntwo\n" {
		t.Fatalf("decoded body = %q", decoded)
	}
}

func TestBuildClientSubscriptionRaw(t *testing.T) {
	subscription, err := BuildClientSubscription(ClientLinksResponse{Links: []ClientLink{{URI: "one"}}}, "raw")
	if err != nil {
		t.Fatalf("BuildClientSubscription: %v", err)
	}
	if subscription.Filename != "veil-subscription-raw.txt" || subscription.Body != "one\n" {
		t.Fatalf("subscription = %+v", subscription)
	}
}

func TestValidateClientSubscriptionQueryRejectsUnknownQueryAndFormat(t *testing.T) {
	if err := ValidateClientSubscriptionQuery(url.Values{"token": []string{"x"}}); err == nil {
		t.Fatalf("expected unknown query error")
	}
	if err := ValidateClientSubscriptionQuery(url.Values{"format": []string{"json"}}); err == nil {
		t.Fatalf("expected invalid format error")
	}
}
