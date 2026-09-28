package api

import (
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/mikkelchokolate/Veil/internal/client"
	"github.com/mikkelchokolate/Veil/internal/protocols"
)

func TestQuotaConfigurationRequiresRealProtocolTrafficProvider(t *testing.T) {
	protocols := []struct {
		name              string
		protocol          string
		transport         string
		port              int
		trafficAccounting bool
		quotaEnforcement  bool
	}{
		{name: "hysteria2", protocol: "hysteria2", transport: "udp", port: 25443, trafficAccounting: true, quotaEnforcement: true},
		{name: "mieru", protocol: "mieru", transport: "tcp", port: 25444, trafficAccounting: true, quotaEnforcement: false},
		{name: "naiveproxy", protocol: "naiveproxy", transport: "tcp", port: 443, trafficAccounting: false, quotaEnforcement: false},
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
			inboundID := "quota-" + protocol.name
			inbound := v1Request(t, router, http.MethodPost, "/api/inbounds", fmt.Sprintf(
				`{"name":%q,"protocol":%q,"transport":%q,"port":%d,"enabled":true}`,
				inboundID, protocol.protocol, protocol.transport, protocol.port))
			if inbound.Code != http.StatusCreated && inbound.Code != http.StatusOK {
				t.Fatalf("create inbound: %d %s", inbound.Code, inbound.Body.String())
			}

			plain := v1Request(t, router, http.MethodPost, "/api/v1/clients", fmt.Sprintf(
				`{"name":%q,"bindings":[{"inboundId":%q,"runtimeIdentity":%q,"credential":"credential"}]}`,
				"plain-"+protocol.name, inboundID, "identity_plain_"+protocol.name))
			if plain.Code != http.StatusCreated {
				t.Fatalf("create unmetered client: %d %s", plain.Code, plain.Body.String())
			}
			plainClient := unwrapClient(t, plain.Body.Bytes())
			bindings, _ := plainClient["bindings"].([]any)
			if len(bindings) != 1 {
				t.Fatalf("binding view = %v", plainClient["bindings"])
			}
			binding, _ := bindings[0].(map[string]any)
			capability, _ := binding["capability"].(map[string]any)
			if got, ok := capability["trafficAccounting"].(bool); !ok || got != protocol.trafficAccounting {
				t.Errorf("trafficAccounting = %v (present=%v), want %v", capability["trafficAccounting"], ok, protocol.trafficAccounting)
			}
			if got, ok := capability["quotaEnforcement"].(bool); !ok || got != protocol.quotaEnforcement {
				t.Errorf("quotaEnforcement = %v (present=%v), want %v", capability["quotaEnforcement"], ok, protocol.quotaEnforcement)
			}

			meteredName := "metered-" + protocol.name
			metered := v1Request(t, router, http.MethodPost, "/api/v1/clients", fmt.Sprintf(
				`{"name":%q,"quotaBytes":1000,"bindings":[{"inboundId":%q,"runtimeIdentity":%q,"credential":"credential"}]}`,
				meteredName, inboundID, "identity_metered_"+protocol.name))
			if protocol.quotaEnforcement {
				if metered.Code != http.StatusCreated {
					t.Fatalf("supported quota create: %d %s", metered.Code, metered.Body.String())
				}
				return
			}
			if metered.Code != http.StatusBadRequest {
				t.Errorf("unsupported quota create status = %d, want 400: %s", metered.Code, metered.Body.String())
			}
			if !strings.Contains(strings.ToLower(metered.Body.String()), "quota") || !strings.Contains(strings.ToLower(metered.Body.String()), "unsupported") {
				t.Errorf("unsupported quota error is not actionable: %s", metered.Body.String())
			}
			clients, _, err := state.clientService.List(clientListFilterAll())
			if err != nil {
				t.Fatal(err)
			}
			for _, current := range clients {
				if current.Name == meteredName {
					t.Error("unsupported metered client was persisted")
				}
			}

			plainID, _ := plainClient["id"].(string)
			plainVersion := int(plainClient["version"].(float64))
			patch := v1Request(t, router, http.MethodPatch, "/api/v1/clients/"+plainID,
				fmt.Sprintf(`{"version":%d,"quotaBytes":1000}`, plainVersion))
			if patch.Code != http.StatusBadRequest {
				t.Errorf("unsupported quota patch status = %d, want 400: %s", patch.Code, patch.Body.String())
			}
			persisted, err := state.clientRepo.Get(plainID)
			if err != nil {
				t.Fatal(err)
			}
			if persisted.QuotaBytes != nil {
				t.Errorf("unsupported quota patch persisted quotaBytes=%d", *persisted.QuotaBytes)
			}
		})
	}
}

// The advertised binding capability and the quota write validator must never
// disagree: both derive from protocolQuotaEnforcement. Iterate every
// registered protocol so a future capability expansion cannot drift from the
// validator's answer without this test failing.
func TestQuotaEnforcementCapabilityMatchesValidator(t *testing.T) {
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
		validator := state.quotaSupportedForInboundLocked(name)
		state.mu.Unlock()
		if capability.QuotaEnforcement != validator {
			t.Errorf("protocol %s: capability quotaEnforcement=%v but validator=%v — drift between the advertised capability and write validation", protocol, capability.QuotaEnforcement, validator)
		}
	}
	// A disabled inbound must fail validation even when its protocol
	// advertises quota enforcement — the capability describes the protocol,
	// not the inbound's enabled state.
	state.mu.Lock()
	state.inbounds = append(state.inbounds, Inbound{
		Name: "cap-disabled-hy2", Protocol: "hysteria2", Enabled: false,
	})
	validatorDisabled := state.quotaSupportedForInboundLocked("cap-disabled-hy2")
	missing := state.quotaSupportedForInboundLocked("cap-unknown")
	state.mu.Unlock()
	if validatorDisabled {
		t.Error("validator must reject quota on a disabled hysteria2 inbound")
	}
	if missing {
		t.Error("validator must reject quota on an unknown inbound")
	}
	if capability := state.bindingCapabilityForInbound("cap-disabled-hy2"); capability == nil || !capability.QuotaEnforcement {
		t.Error("capability must keep advertising hysteria2 quota enforcement regardless of inbound enabled state")
	}
}

// clientListFilterAll keeps this regression independent of default pagination.
func clientListFilterAll() client.ListFilter {
	return client.ListFilter{PageSize: 1000}
}
