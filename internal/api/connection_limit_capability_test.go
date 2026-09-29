package api

import (
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/mikkelchokolate/Veil/internal/protocols"
)

// #1173: deviceLimit/ipLimit are enforceable only by protocols whose runtime
// admits sessions through an authentication hook. The advertised binding
// capability, the /api/protocols catalog, and every write validator must
// derive from one verdict (protocols.TelemetrySupportOf) — mirroring the
// quota-capability regression coverage so the surfaces cannot drift.

func TestConnectionLimitsRequireCapableProtocol(t *testing.T) {
	protocols := []struct {
		name         string
		protocol     string
		transport    string
		port         int
		deviceLimits bool
	}{
		{name: "hysteria2", protocol: "hysteria2", transport: "udp", port: 26443, deviceLimits: true},
		{name: "mieru", protocol: "mieru", transport: "tcp", port: 26444, deviceLimits: false},
		{name: "naiveproxy", protocol: "naiveproxy", transport: "tcp", port: 26445, deviceLimits: false},
	}

	for _, protocol := range protocols {
		t.Run(protocol.name, func(t *testing.T) {
			router, state := newApplyTrackedRouterWithState(t)
			t.Cleanup(func() { _ = state.Close() })
			if protocol.protocol == "naiveproxy" {
				state.mu.Lock()
				state.settings.Email = "admin@example.com"
				state.settings.DefaultAcmeEmail = "admin@example.com"
				state.mu.Unlock()
			}
			inboundID := "limits-" + protocol.name
			inbound := v1Request(t, router, http.MethodPost, "/api/inbounds", fmt.Sprintf(
				`{"name":%q,"protocol":%q,"transport":%q,"port":%d,"enabled":true}`,
				inboundID, protocol.protocol, protocol.transport, protocol.port))
			if inbound.Code != http.StatusCreated && inbound.Code != http.StatusOK {
				t.Fatalf("create inbound: %d %s", inbound.Code, inbound.Body.String())
			}

			// Unlimited clients keep working on every protocol.
			plain := v1Request(t, router, http.MethodPost, "/api/v1/clients", fmt.Sprintf(
				`{"name":%q,"bindings":[{"inboundId":%q,"runtimeIdentity":%q,"credential":"credential"}]}`,
				"plain-"+protocol.name, inboundID, "identity_plain_"+protocol.name))
			if plain.Code != http.StatusCreated {
				t.Fatalf("create unlimited client: %d %s", plain.Code, plain.Body.String())
			}
			plainClient := unwrapClient(t, plain.Body.Bytes())
			bindings, _ := plainClient["bindings"].([]any)
			if len(bindings) != 1 {
				t.Fatalf("binding view = %v", plainClient["bindings"])
			}
			binding, _ := bindings[0].(map[string]any)
			capability, _ := binding["capability"].(map[string]any)
			if got, ok := capability["deviceLimits"].(bool); !ok || got != protocol.deviceLimits {
				t.Errorf("deviceLimits = %v (present=%v), want %v", capability["deviceLimits"], ok, protocol.deviceLimits)
			}

			limitedName := "limited-" + protocol.name
			limited := v1Request(t, router, http.MethodPost, "/api/v1/clients", fmt.Sprintf(
				`{"name":%q,"deviceLimit":2,"ipLimit":1,"bindings":[{"inboundId":%q,"runtimeIdentity":%q,"credential":"credential"}]}`,
				limitedName, inboundID, "identity_limited_"+protocol.name))
			if protocol.deviceLimits {
				if limited.Code != http.StatusCreated {
					t.Fatalf("supported limits create: %d %s", limited.Code, limited.Body.String())
				}
				limitedClient := unwrapClient(t, limited.Body.Bytes())
				if got, ok := limitedClient["deviceLimit"].(float64); !ok || int(got) != 2 {
					t.Errorf("deviceLimit = %v (present=%v), want 2", limitedClient["deviceLimit"], ok)
				}
				if got, ok := limitedClient["ipLimit"].(float64); !ok || int(got) != 1 {
					t.Errorf("ipLimit = %v (present=%v), want 1", limitedClient["ipLimit"], ok)
				}
			} else {
				if limited.Code != http.StatusBadRequest {
					t.Errorf("unsupported limits create status = %d, want 400: %s", limited.Code, limited.Body.String())
				}
				if !strings.Contains(strings.ToLower(limited.Body.String()), "unsupported") {
					t.Errorf("unsupported limits error is not actionable: %s", limited.Body.String())
				}
			}

			// Patch must reject limits on a client bound to an incapable inbound.
			plainID, _ := plainClient["id"].(string)
			plainVersion := int(plainClient["version"].(float64))
			patch := v1Request(t, router, http.MethodPatch, "/api/v1/clients/"+plainID,
				fmt.Sprintf(`{"version":%d,"deviceLimit":3}`, plainVersion))
			if protocol.deviceLimits {
				if patch.Code != http.StatusOK {
					t.Errorf("supported limits patch status = %d, want 200: %s", patch.Code, patch.Body.String())
				}
			} else {
				if patch.Code != http.StatusBadRequest {
					t.Errorf("unsupported limits patch status = %d, want 400: %s", patch.Code, patch.Body.String())
				}
				persisted, err := state.clientRepo.Get(plainID)
				if err != nil {
					t.Fatal(err)
				}
				if persisted.DeviceLimit != nil {
					t.Errorf("unsupported limits patch persisted deviceLimit=%d", *persisted.DeviceLimit)
				}
			}
		})
	}
}

// Attaching a limit-carrying client to an incapable inbound, and enabling a
// previously-disabled incapable binding on one, must both fail validation —
// otherwise the stored limit is silently inert for the sessions that binding
// admits.
func TestConnectionLimitBindingMutationsValidate(t *testing.T) {
	router, state := newApplyTrackedRouterWithState(t)
	t.Cleanup(func() { _ = state.Close() })

	mk := func(name, protocol, transport string, port int) {
		resp := v1Request(t, router, http.MethodPost, "/api/inbounds", fmt.Sprintf(
			`{"name":%q,"protocol":%q,"transport":%q,"port":%d,"enabled":true}`,
			name, protocol, transport, port))
		if resp.Code != http.StatusCreated && resp.Code != http.StatusOK {
			t.Fatalf("create inbound %s: %d %s", name, resp.Code, resp.Body.String())
		}
	}
	mk("lim-hy2", "hysteria2", "udp", 27443)
	mk("lim-mieru", "mieru", "tcp", 27444)

	created := v1Request(t, router, http.MethodPost, "/api/v1/clients",
		`{"name":"lim-attach","deviceLimit":2,"bindings":[{"inboundId":"lim-hy2","runtimeIdentity":"lim_attach","credential":"credential"}]}`)
	if created.Code != http.StatusCreated {
		t.Fatalf("create limited client: %d %s", created.Code, created.Body.String())
	}
	client := unwrapClient(t, created.Body.Bytes())
	clientID, _ := client["id"].(string)

	// Attach to mieru while limited — reject.
	attach := v1Request(t, router, http.MethodPost, "/api/v1/clients/"+clientID+"/bindings",
		`{"inboundId":"lim-mieru","credential":"credential"}`)
	if attach.Code != http.StatusBadRequest {
		t.Fatalf("attach to incapable inbound status = %d, want 400: %s", attach.Code, attach.Body.String())
	}

	// A disabled binding is inert: creating it disabled on mieru is allowed,
	// but enabling it while the client is limited must reject.
	created2 := v1Request(t, router, http.MethodPost, "/api/v1/clients",
		`{"name":"lim-enable","deviceLimit":2,"bindings":[{"inboundId":"lim-hy2","runtimeIdentity":"lim_enable","credential":"credential"},{"inboundId":"lim-mieru","enabled":false,"credential":"credential"}]}`)
	if created2.Code != http.StatusCreated {
		t.Fatalf("create client with disabled mieru binding: %d %s", created2.Code, created2.Body.String())
	}
	client2 := unwrapClient(t, created2.Body.Bytes())
	for _, b := range client2["bindings"].([]any) {
		binding, _ := b.(map[string]any)
		if binding["inboundId"] != "lim-mieru" {
			continue
		}
		bindingID, _ := binding["id"].(string)
		enable := v1Request(t, router, http.MethodPatch,
			"/api/v1/clients/"+client2["id"].(string)+"/bindings/"+bindingID,
			fmt.Sprintf(`{"enabled":true,"version":%d}`, int(binding["version"].(float64))))
		if enable.Code != http.StatusBadRequest {
			t.Fatalf("enable incapable binding on limited client status = %d, want 400: %s", enable.Code, enable.Body.String())
		}
	}

	// Range guard: zero and negative limits are validation errors, not
	// "unlimited".
	for _, bad := range []string{`{"name":"bad0","deviceLimit":0}`, `{"name":"badneg","ipLimit":-2}`} {
		resp := v1Request(t, router, http.MethodPost, "/api/v1/clients", bad)
		if resp.Code != http.StatusBadRequest {
			t.Errorf("non-positive limit create status = %d, want 400: %s", resp.Code, resp.Body.String())
		}
	}
	patchBad := v1Request(t, router, http.MethodPatch, "/api/v1/clients/"+clientID,
		fmt.Sprintf(`{"version":%d,"ipLimit":0}`, int(client["version"].(float64))))
	if patchBad.Code != http.StatusBadRequest {
		t.Errorf("non-positive limit patch status = %d, want 400: %s", patchBad.Code, patchBad.Body.String())
	}
}

// The advertised binding capability and the connection-limit write validator
// must never disagree: both derive from TelemetrySupportOf.DeviceLimits.
// Iterate every registered protocol so a future capability expansion cannot
// drift from the validator's answer without this test failing.
func TestConnectionLimitCapabilityMatchesValidator(t *testing.T) {
	state := &managementState{}
	for _, protocol := range protocols.NewRegistry().Protocols() {
		name := "cap-" + protocol
		state.mu.Lock()
		state.inbounds = append(state.inbounds, Inbound{
			Name: name, Protocol: protocol, Enabled: true,
		})
		state.mu.Unlock()

		capability := state.bindingCapabilityForInbound(name)
		if capability == nil {
			t.Fatalf("protocol %s: binding capability is nil", protocol)
		}
		state.mu.Lock()
		validator := state.connectionLimitsSupportedForInboundLocked(name)
		state.mu.Unlock()
		if capability.DeviceLimits != validator {
			t.Errorf("protocol %s: capability deviceLimits=%v but validator=%v — drift between the advertised capability and write validation", protocol, capability.DeviceLimits, validator)
		}
	}
	// A disabled inbound must fail validation even when its protocol
	// advertises connection-limit enforcement — the capability describes the
	// protocol, not the inbound's enabled state.
	state.mu.Lock()
	state.inbounds = append(state.inbounds, Inbound{
		Name: "cap-disabled-hy2", Protocol: "hysteria2", Enabled: false,
	})
	validatorDisabled := state.connectionLimitsSupportedForInboundLocked("cap-disabled-hy2")
	missing := state.connectionLimitsSupportedForInboundLocked("cap-unknown")
	state.mu.Unlock()
	if validatorDisabled {
		t.Error("validator must reject connection limits on a disabled hysteria2 inbound")
	}
	if missing {
		t.Error("validator must reject connection limits on an unknown inbound")
	}
	if capability := state.bindingCapabilityForInbound("cap-disabled-hy2"); capability == nil || !capability.DeviceLimits {
		t.Error("capability must keep advertising hysteria2 connection limits regardless of inbound enabled state")
	}
}
