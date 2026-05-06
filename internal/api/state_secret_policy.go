package api

// stateSecretPolicy is the State store Module that knows which management
// snapshot fields are secrets. StateStore supplies encryption/decryption as an
// Adapter function, while this Module preserves locality for secret field policy.
type stateSecretPolicy struct{}

func (stateSecretPolicy) Transform(snapshot *managementSnapshot, transform func(string) string) {
	if snapshot == nil || transform == nil {
		return
	}
	snapshot.Settings.NaivePassword = transform(snapshot.Settings.NaivePassword)
	snapshot.Settings.Hysteria2Password = transform(snapshot.Settings.Hysteria2Password)
	for i := range snapshot.Inbounds {
		snapshot.Inbounds[i].Password = transform(snapshot.Inbounds[i].Password)
		for j := range snapshot.Inbounds[i].Profiles {
			snapshot.Inbounds[i].Profiles[j].Password = transform(snapshot.Inbounds[i].Profiles[j].Password)
		}
	}
	snapshot.Warp.LicenseKey = transform(snapshot.Warp.LicenseKey)
	snapshot.Warp.PrivateKey = transform(snapshot.Warp.PrivateKey)
}
