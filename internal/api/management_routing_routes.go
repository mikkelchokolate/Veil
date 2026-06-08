package api

import (
	"context"
	"net/http"

	"github.com/mikkelchokolate/Veil/internal/managementstate"
	"github.com/mikkelchokolate/Veil/internal/routing"
	veilwarp "github.com/mikkelchokolate/Veil/internal/warp"
)

// warpRegisterFunc provisions a free Cloudflare WARP account so enabling WARP
// needs no manual key/license. It is a package variable so tests can swap in a
// fake registrar.
var warpRegisterFunc = func(ctx context.Context) (veilwarp.Registration, error) {
	return veilwarp.NewRegistrar().Register(ctx)
}

func (s *managementState) handleRoutingRules(w http.ResponseWriter, r *http.Request) {
	_ = s.withMutation(func(mutation managementstate.Mutation) error {
		switch r.Method {
		case http.MethodGet:
			writeJSON(w, mutation.RoutingRules())
		case http.MethodPost:
			var rule RoutingRule
			if !decodeJSONRequest(w, r, &rule) {
				return nil
			}
			created, err := mutation.CreateRoutingRule(rule)
			s.logUserAction(r, "create_routing_rule", rule.Name, err == nil, "")
			if err != nil {
				writeRoutingRuleManagementError(w, err)
				return nil
			}
			writeJSONStatus(w, http.StatusCreated, created)
		default:
			methodNotAllowed(w, http.MethodGet, http.MethodPost)
		}
		return nil
	})
}

func (s *managementState) handleRoutingRuleByName(w http.ResponseWriter, r *http.Request) {
	name, ok := managementstate.NewResourceNameParser("/api/routing/rules/").Parse(r.URL.Path)
	if !ok {
		writeNotFound(w)
		return
	}
	_ = s.withMutation(func(mutation managementstate.Mutation) error {
		rule, ok := mutation.RoutingRule(name)
		if !ok {
			writeNotFound(w)
			return nil
		}
		switch r.Method {
		case http.MethodGet:
			writeJSON(w, rule)
		case http.MethodPut:
			var update RoutingRule
			if !decodeJSONRequest(w, r, &update) {
				return nil
			}
			updated, err := mutation.UpdateRoutingRule(name, update)
			s.logUserAction(r, "update_routing_rule", name, err == nil, "")
			if err != nil {
				writeRoutingRuleManagementError(w, err)
				return nil
			}
			writeJSON(w, updated)
		case http.MethodDelete:
			err := mutation.DeleteRoutingRule(name)
			s.logUserAction(r, "delete_routing_rule", name, err == nil, "")
			if err != nil {
				writeRoutingRuleManagementError(w, err)
				return nil
			}
			w.WriteHeader(http.StatusNoContent)
		default:
			methodNotAllowed(w, http.MethodGet, http.MethodPut, http.MethodDelete)
		}
		return nil
	})
}

func writeRoutingRuleManagementError(w http.ResponseWriter, err error) {
	switch err {
	case routing.ErrRoutingRuleInvalid:
		writeError(w, "name, match, and outbound are required", http.StatusBadRequest)
	case routing.ErrRoutingRuleDuplicateName:
		writeError(w, "routing rule name already exists", http.StatusConflict)
	case routing.ErrRoutingRuleNotFound:
		writeNotFound(w)
	default:
		writeError(w, err.Error(), http.StatusInternalServerError)
	}
}

func (s *managementState) handleRoutingPresets(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w, http.MethodGet)
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	writeJSON(w, routing.NewRoutingPresetResponseBuilder(s.routingPreset, s.routingSource, s.rules).WithPresets(routing.PresetProfiles()).Build())
}

func (s *managementState) handleRoutingPresetByName(w http.ResponseWriter, r *http.Request) {
	name, ok := managementstate.NewResourceNameParser("/api/routing/presets/").Parse(r.URL.Path)
	if !ok {
		writeNotFound(w)
		return
	}
	preset, ok := routing.PresetByName(name)
	if !ok {
		writeNotFound(w)
		return
	}
	if r.Method != http.MethodPost {
		methodNotAllowed(w, http.MethodPost)
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	state := routing.RoutingPresetState{ActivePreset: s.routingPreset, Source: s.routingSource, Rules: s.rules}
	routing.NewRoutingPresetApplication(&state).Apply(preset)
	if s.warp.Enabled {
		warpRule := RoutingRule{
			Name:     "warp-routing",
			Match:    "geosite:openai",
			Outbound: "warp",
			Enabled:  true,
		}
		state.Rules = append([]RoutingRule{warpRule}, state.Rules...)
	}
	s.routingPreset = state.ActivePreset
	s.routingSource = state.Source
	s.rules = state.Rules
	err := s.saveLocked()
	s.logUserAction(r, "apply_routing_preset", name, err == nil, "")
	if err != nil {
		writeError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, routing.NewRoutingPresetResponseBuilder(s.routingPreset, s.routingSource, s.rules).Build())
}

func (s *managementState) handleWarp(w http.ResponseWriter, r *http.Request) {
	_ = s.withMutation(func(mutation managementstate.Mutation) error {
		switch r.Method {
		case http.MethodGet:
			writeJSON(w, mutation.Warp())
		case http.MethodPut:
			var warp WarpConfig
			if !decodeJSONRequest(w, r, &warp) {
				return nil
			}
			// Resolve provisioned fields against stored state. A stale or partial
			// form submit can re-send the "[REDACTED]" key placeholder and omit
			// non-secret fields (peer key, address, reserved); fill those from
			// what we already hold so re-enabling keeps the existing account
			// instead of coming up with an empty, non-functional config.
			warp = veilwarp.PreserveRedacted(warp, s.warp)
			if warp.PeerPublicKey == "" {
				warp.PeerPublicKey = s.warp.PeerPublicKey
			}
			if warp.LocalAddress == "" {
				warp.LocalAddress = s.warp.LocalAddress
			}
			if len(warp.Reserved) == 0 {
				warp.Reserved = s.warp.Reserved
			}
			// Provision a free Cloudflare WARP account whenever the toggle is on
			// but the config is still incomplete, so the operator only flips the
			// toggle — no key, license, or peer details to enter.
			if warp.Enabled && (warp.PrivateKey == "" || warp.PeerPublicKey == "" || warp.LocalAddress == "") {
				reg, err := warpRegisterFunc(r.Context())
				if err != nil {
					s.logUserAction(r, "update_warp", "warp", false, "registration failed")
					writeError(w, "WARP registration failed: "+err.Error(), http.StatusBadGateway)
					return nil
				}
				warp.PrivateKey = reg.PrivateKey
				warp.PeerPublicKey = reg.PeerPublicKey
				warp.LocalAddress = reg.LocalAddress
				warp.Reserved = reg.Reserved
				if reg.Endpoint != "" {
					warp.Endpoint = reg.Endpoint
				}
				if reg.License != "" {
					warp.LicenseKey = reg.License
				}
			}
			candidateWarp := s.warp
			candidateRules := append([]RoutingRule(nil), s.rules...)
			candidateMutation := managementstate.NewMutation(managementstate.MutationTarget{
				Warp:  &candidateWarp,
				Rules: &candidateRules,
			}, nil)
			if _, err := candidateMutation.UpdateWarp(warp); err != nil {
				writeError(w, err.Error(), http.StatusBadRequest)
				return nil
			}
			if validation, ok := s.enforceValidationLocked(r.Context(), s.settings, s.inbounds, candidateWarp); !ok {
				s.logUserAction(r, "update_warp", "warp", false, "live validation failed")
				writeValidationFailure(w, validation)
				return nil
			}
			updated, err := mutation.UpdateWarp(warp)
			s.logUserAction(r, "update_warp", "warp", err == nil, "")
			if err != nil {
				writeError(w, err.Error(), http.StatusInternalServerError)
				return nil
			}
			writeJSON(w, updated)
		default:
			methodNotAllowed(w, http.MethodGet, http.MethodPut)
		}
		return nil
	})
}
