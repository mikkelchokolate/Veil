package api

import "net/http"

func (s *managementState) handleRoutingRules(w http.ResponseWriter, r *http.Request) {
	_ = s.withTransaction(func(tx *managementTransaction) error {
		mutation := tx.Mutation()
		switch r.Method {
		case http.MethodGet:
			writeJSON(w, mutation.RoutingRules())
		case http.MethodPost:
			var rule RoutingRule
			if !decodeJSONRequest(w, r, &rule) {
				return nil
			}
			created, err := mutation.CreateRoutingRule(rule)
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
	name, ok := NewManagementResourceName("/api/routing/rules/").Parse(r.URL.Path)
	if !ok {
		writeNotFound(w)
		return
	}
	_ = s.withTransaction(func(tx *managementTransaction) error {
		mutation := tx.Mutation()
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
			if err != nil {
				writeRoutingRuleManagementError(w, err)
				return nil
			}
			writeJSON(w, updated)
		case http.MethodDelete:
			if err := mutation.DeleteRoutingRule(name); err != nil {
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

func (s *managementState) handleRoutingPresets(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w, http.MethodGet)
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	writeJSON(w, NewRoutingPresetResponseBuilder(s.routingPreset, s.routingSource, s.rules).WithPresets(routingPresetProfiles()).Build())
}

func (s *managementState) handleRoutingPresetByName(w http.ResponseWriter, r *http.Request) {
	name, ok := NewManagementResourceName("/api/routing/presets/").Parse(r.URL.Path)
	if !ok {
		writeNotFound(w)
		return
	}
	preset, ok := routingPresetByName(name)
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
	state := RoutingPresetState{ActivePreset: s.routingPreset, Source: s.routingSource, Rules: s.rules}
	NewRoutingPresetApplication(&state).Apply(preset)
	s.routingPreset = state.ActivePreset
	s.routingSource = state.Source
	s.rules = state.Rules
	if err := s.saveLocked(); err != nil {
		writeError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, NewRoutingPresetResponseBuilder(s.routingPreset, s.routingSource, s.rules).Build())
}

func (s *managementState) handleWarp(w http.ResponseWriter, r *http.Request) {
	_ = s.withTransaction(func(tx *managementTransaction) error {
		mutation := tx.Mutation()
		switch r.Method {
		case http.MethodGet:
			writeJSON(w, mutation.Warp())
		case http.MethodPut:
			var warp WarpConfig
			if !decodeJSONRequest(w, r, &warp) {
				return nil
			}
			updated, err := mutation.UpdateWarp(warp)
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
