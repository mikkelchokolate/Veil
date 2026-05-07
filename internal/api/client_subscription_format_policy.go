package api

import "fmt"

type ClientSubscriptionFormatPolicy struct{}

func NewClientSubscriptionFormatPolicy() ClientSubscriptionFormatPolicy {
	return ClientSubscriptionFormatPolicy{}
}

func (ClientSubscriptionFormatPolicy) Normalize(format string) (string, error) {
	if format == "" {
		return "base64", nil
	}
	if format == "base64" || format == "raw" {
		return format, nil
	}
	return "", fmt.Errorf("format must be base64 or raw")
}
