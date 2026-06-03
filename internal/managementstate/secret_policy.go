package managementstate

import "github.com/veil-panel/veil/internal/model"

// SecretPolicy is the State store Module that knows which Management state
// snapshot fields are secrets. Store supplies encryption/decryption as an
// Adapter function, while this Module preserves locality for secret field policy.
type SecretPolicy struct{}

func (SecretPolicy) Transform(snapshot *model.ManagementSnapshot, transform func(string) string) {
	if snapshot == nil || transform == nil {
		return
	}
	snapshot.Settings.NaivePassword = transform(snapshot.Settings.NaivePassword)
	snapshot.Settings.Hysteria2Password = transform(snapshot.Settings.Hysteria2Password)
	snapshot.Settings.OlcrtcAuth = transform(snapshot.Settings.OlcrtcAuth)
	for i := range snapshot.Inbounds {
		snapshot.Inbounds[i].Password = transform(snapshot.Inbounds[i].Password)
		snapshot.Inbounds[i].OlcrtcAuth = transform(snapshot.Inbounds[i].OlcrtcAuth)
		for j := range snapshot.Inbounds[i].Profiles {
			snapshot.Inbounds[i].Profiles[j].Password = transform(snapshot.Inbounds[i].Profiles[j].Password)
		}
	}
	snapshot.Warp.LicenseKey = transform(snapshot.Warp.LicenseKey)
	snapshot.Warp.PrivateKey = transform(snapshot.Warp.PrivateKey)
}
