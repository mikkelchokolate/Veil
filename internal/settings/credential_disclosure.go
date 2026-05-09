package settings

import "strings"

const RedactedSecret = "[REDACTED]"

type CredentialDisclosure struct{}

func NewCredentialDisclosure() CredentialDisclosure { return CredentialDisclosure{} }

func (CredentialDisclosure) Redact(secret string) string {
	if secret == "" {
		return ""
	}
	return RedactedSecret
}

func (CredentialDisclosure) PreserveRedacted(update, current string) string {
	if update == RedactedSecret {
		return current
	}
	return update
}

func (CredentialDisclosure) RedactText(text string, secrets []string) string {
	for _, secret := range secrets {
		if secret == "" {
			continue
		}
		text = strings.ReplaceAll(text, secret, RedactedSecret)
	}
	return text
}
