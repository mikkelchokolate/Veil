package managementstate

import "github.com/mikkelchokolate/Veil/internal/model"

// SecretPolicy is the State store Module that knows which Management state
// snapshot fields are secrets. Store supplies encryption/decryption as an
// Adapter function, while this Module preserves locality for secret field policy.
type SecretPolicy struct{}

func (SecretPolicy) Transform(snapshot *model.ManagementSnapshot, transform func(string) (string, error)) error {
	if snapshot == nil || transform == nil {
		return nil
	}
	var err error
	if snapshot.Settings.NaivePassword, err = transform(snapshot.Settings.NaivePassword); err != nil {
		return err
	}
	if snapshot.Settings.Hysteria2Password, err = transform(snapshot.Settings.Hysteria2Password); err != nil {
		return err
	}
	if snapshot.Settings.OlcrtcAuth, err = transform(snapshot.Settings.OlcrtcAuth); err != nil {
		return err
	}
	for i := range snapshot.Inbounds {
		if snapshot.Inbounds[i].Password, err = transform(snapshot.Inbounds[i].Password); err != nil {
			return err
		}
		if snapshot.Inbounds[i].OlcrtcAuth, err = transform(snapshot.Inbounds[i].OlcrtcAuth); err != nil {
			return err
		}
		for j := range snapshot.Inbounds[i].Profiles {
			if snapshot.Inbounds[i].Profiles[j].Password, err = transform(snapshot.Inbounds[i].Profiles[j].Password); err != nil {
				return err
			}
		}
	}
	if snapshot.Warp.LicenseKey, err = transform(snapshot.Warp.LicenseKey); err != nil {
		return err
	}
	if snapshot.Warp.PrivateKey, err = transform(snapshot.Warp.PrivateKey); err != nil {
		return err
	}
	return nil
}
