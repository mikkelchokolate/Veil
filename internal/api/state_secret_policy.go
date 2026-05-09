package api

import "github.com/veil-panel/veil/internal/managementstate"

// stateSecretPolicy is the State store Module that knows which management
// snapshot fields are secrets. StateStore supplies encryption/decryption as an
// Adapter function, while this Module preserves locality for secret field policy.
type stateSecretPolicy struct{}

func (stateSecretPolicy) Transform(snapshot *managementSnapshot, transform func(string) string) {
	managementstate.SecretPolicy{}.Transform(snapshot, transform)
}
