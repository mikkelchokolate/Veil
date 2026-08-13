package managementstate

import (
	"reflect"

	"github.com/mikkelchokolate/Veil/internal/model"
	"github.com/mikkelchokolate/Veil/internal/protocols"
	"github.com/mikkelchokolate/Veil/internal/protocols/schema"
	veilsettings "github.com/mikkelchokolate/Veil/internal/settings"
)

// SecretPolicy is the State store Module that knows which Management state
// snapshot fields are secrets. Store supplies encryption/decryption as an
// Adapter function, while this Module preserves locality for secret field policy.
//
// After the plugin-based protocol refactor, protocol-specific secrets live in
// the ProtocolFields maps as well as the legacy flat fields. SecretPolicy uses
// the registered protocol plugins' UI schemas to discover which keys are
// password-typed and therefore need encryption/decryption.
type SecretPolicy struct {
	settingsFieldSchemas []schema.FieldSchema
	inboundFieldSchemas  []schema.FieldSchema
}

func NewSecretPolicy() SecretPolicy {
	registry := protocols.NewRegistry()
	return SecretPolicy{
		settingsFieldSchemas: registry.SettingsFieldSchemas(),
		inboundFieldSchemas:  registry.InboundFieldSchemas(),
	}
}

func (p SecretPolicy) Transform(snapshot *model.ManagementSnapshot, transform func(string) (string, error)) error {
	if snapshot == nil || transform == nil {
		return nil
	}
	var err error

	// Transform password-typed protocol fields in settings, both in the dynamic
	// ProtocolFields map and in the legacy flat fields. Multiple protocols may
	// declare the same key (for example "password"), so deduplicate by key to
	// avoid transforming one value more than once.
	seenSettingsKeys := make(map[string]bool)
	for _, f := range p.settingsFieldSchemas {
		if f.Type != schema.FieldPassword || seenSettingsKeys[f.Key] {
			continue
		}
		seenSettingsKeys[f.Key] = true
		if snapshot.Settings.ProtocolFields != nil {
			if pfValue, ok := snapshot.Settings.ProtocolFields[f.Key].(string); ok {
				if transformed, err := transform(pfValue); err != nil {
					return err
				} else {
					snapshot.Settings.ProtocolFields[f.Key] = transformed
				}
			}
		}
		field := reflect.ValueOf(&snapshot.Settings).Elem().FieldByName(veilsettings.StructFieldName(f.Key))
		if field.IsValid() && field.Kind() == reflect.String {
			if transformed, err := transform(field.String()); err != nil {
				return err
			} else {
				field.SetString(transformed)
			}
		}
	}

	for i := range snapshot.Inbounds {
		inbound := &snapshot.Inbounds[i]

		// Transform password-typed protocol fields in the inbound, both in the
		// dynamic ProtocolFields map and in the legacy flat fields. Deduplicate
		// by key: several protocols (mieru, olcrtc) expose the generic
		// "password" key, and it must be transformed exactly once.
		seenInboundKeys := make(map[string]bool)
		passwordViaSchema := false
		for _, f := range p.inboundFieldSchemas {
			if f.Type != schema.FieldPassword || seenInboundKeys[f.Key] {
				continue
			}
			seenInboundKeys[f.Key] = true
			if inbound.ProtocolFields != nil {
				if pfValue, ok := inbound.ProtocolFields[f.Key].(string); ok {
					if transformed, err := transform(pfValue); err != nil {
						return err
					} else {
						inbound.ProtocolFields[f.Key] = transformed
					}
				}
			}
			field := reflect.ValueOf(inbound).Elem().FieldByName(veilsettings.StructFieldName(f.Key))
			if field.IsValid() && field.Kind() == reflect.String {
				if transformed, err := transform(field.String()); err != nil {
					return err
				} else {
					field.SetString(transformed)
				}
				if veilsettings.StructFieldName(f.Key) == "Password" {
					passwordViaSchema = true
				}
			}
		}

		// The inbound password and profile passwords are generic fields used by
		// protocols such as mieru and olcrtc and are not part of ProtocolFields.
		// When a password-typed schema key maps onto the flat Password field
		// (for example the shared "password" key), it was already transformed
		// above, so do not transform it a second time.
		if !passwordViaSchema {
			if inbound.Password, err = transform(inbound.Password); err != nil {
				return err
			}
		}
		for j := range inbound.Profiles {
			if inbound.Profiles[j].Password, err = transform(inbound.Profiles[j].Password); err != nil {
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
