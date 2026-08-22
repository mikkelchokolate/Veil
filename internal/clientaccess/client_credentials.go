package clientaccess

// ClientCredential carries per-client credential material resolved from the
// normalized Client+Binding+Credential store or legacy inbound profiles.
type ClientCredential struct {
	Name     string
	Username string
	Password string
}

// BuildClientCredentials resolves the effective client credentials for an
// inbound: enabled legacy inbound-embedded profiles, overridden by any
// normalized Client+Binding credentials attached as runtime-only data (the
// RuntimeCredentials field). Normalized credentials replace profiles with the
// same username, mirroring generatedconfig's merge, so every consumer (links,
// subscriptions, live validation, renderers) sees the same user set.
func BuildClientCredentials(inbound Inbound) ([]ClientCredential, error) {
	credentials, err := NewInboundCredentialPolicy(nil).ClientCredentials(inbound)
	if err != nil {
		return nil, err
	}
	if len(inbound.RuntimeCredentials) == 0 {
		return credentials, nil
	}
	overrides := make(map[string]struct{}, len(inbound.RuntimeCredentials))
	for _, credential := range inbound.RuntimeCredentials {
		overrides[credential.Username] = struct{}{}
	}
	merged := make([]ClientCredential, 0, len(credentials)+len(inbound.RuntimeCredentials))
	for _, credential := range credentials {
		if _, replaced := overrides[credential.Username]; !replaced {
			merged = append(merged, credential)
		}
	}
	for _, credential := range inbound.RuntimeCredentials {
		merged = append(merged, ClientCredential{Name: credential.Name, Username: credential.Username, Password: credential.Password})
	}
	return merged, nil
}
