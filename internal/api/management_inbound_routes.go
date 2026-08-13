package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/mikkelchokolate/Veil/internal/inbounds"
	"github.com/mikkelchokolate/Veil/internal/managementstate"
	"github.com/mikkelchokolate/Veil/internal/protocols"
	veilsettings "github.com/mikkelchokolate/Veil/internal/settings"
)

// decodeSettingsBody decodes a settings PUT body with the same strictness as
// decodeJSONRequest (unknown fields rejected) but from an already-read byte
// slice, so the handler can also inspect which fields were present. Parse
// errors are masked to a generic message, matching decodeJSONRequest.
func decodeSettingsBody(body []byte, v *Settings) error {
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(v); err != nil {
		return fmt.Errorf("invalid JSON: %w", err)
	}
	return nil
}

// inheritMissingSettings returns a copy of update where top-level settings
// fields that were NOT present in the request body keep their current live
// value. The PUT endpoint decodes into a Settings struct, which cannot
// distinguish "field omitted" from "field set to its zero value"; without this
// step a client that sends a partial payload (e.g. the legacy server-rendered
// panel, which only submits panelListen/panelAccess/webBasePath/mode/domain/
// email/protocolFields) would silently zero out firewallManagement, the port
// defaults and the ACME fields on every save.
//
// Only truly non-schema fields are inherited here. Schema-declared keys
// (panelDomain, panelEmail, panelPublicPort, protocol credentials) are
// resolved by normalizeProtocolFields from their protocolFields copy, which
// the legacy panel submits; inheriting their flat value here would override a
// fresh protocolFields edit with the stale flat value.
func inheritMissingSettings(update, current Settings, body []byte) Settings {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(body, &raw); err != nil {
		return update
	}
	inherited := update
	if _, ok := raw["firewallManagement"]; !ok {
		inherited.FirewallManagement = current.FirewallManagement
	}
	if _, ok := raw["defaultInboundPublicPort"]; !ok {
		inherited.DefaultInboundPublicPort = current.DefaultInboundPublicPort
	}
	if _, ok := raw["defaultAcmeEmail"]; !ok {
		inherited.DefaultAcmeEmail = current.DefaultAcmeEmail
	}
	if _, ok := raw["acmeChallengeMode"]; !ok {
		inherited.AcmeChallengeMode = current.AcmeChallengeMode
	}
	return inherited
}

func (s *managementState) handleSettings(w http.ResponseWriter, r *http.Request) {
	_ = s.withMutation(func(mutation managementstate.Mutation) error {
		switch r.Method {
		case http.MethodGet:
			settings := mutation.Settings()
			if role, _ := r.Context().Value(contextKeyRole).(string); role == "viewer" {
				writeJSON(w, newViewerSettingsMetadata(settings))
			} else {
				writeJSON(w, settings)
			}
		case http.MethodPut:
			ct := r.Header.Get("Content-Type")
			if ct != "" && !isJSONMediaType(ct) {
				writeError(w, "Unsupported Media Type: Content-Type must be application/json", http.StatusUnsupportedMediaType)
				return nil
			}
			body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxJSONBodyBytes))
			if err != nil {
				var maxBytesErr *http.MaxBytesError
				if errors.As(err, &maxBytesErr) {
					writeError(w, "Request body too large", http.StatusRequestEntityTooLarge)
				} else {
					writeError(w, "Failed to read request body", http.StatusBadRequest)
				}
				return nil
			}
			var settings Settings
			if err := decodeSettingsBody(body, &settings); err != nil {
				writeError(w, err.Error(), http.StatusBadRequest)
				return nil
			}
			candidate := inheritMissingSettings(settings, s.settings, body)
			if err := veilsettings.NewSettingsValidationWithFieldSchemas(protocols.NewRegistry().SettingsFieldSchemas()).NormalizeAndValidate(&candidate, s.settings); err != nil {
				writeError(w, err.Error(), http.StatusBadRequest)
				return nil
			}
			if validation, ok := s.enforceValidationLocked(r.Context(), candidate, s.inbounds, s.warp); !ok {
				s.logUserAction(r, "update_settings", "settings", false, "live validation failed")
				writeValidationFailure(w, validation)
				return nil
			}
			updated, err := mutation.UpdateSettings(candidate)
			s.logUserAction(r, "update_settings", "settings", err == nil, "")
			if err != nil {
				writeError(w, err.Error(), http.StatusBadRequest)
				return nil
			}
			actor, _ := r.Context().Value(contextKeyUsername).(string)
			outcome := s.autoApplyResultLocked(r, actor)
			s.writeMutationResponse(w, http.StatusOK, updated, outcome)
		default:
			methodNotAllowed(w, http.MethodGet, http.MethodPut)
		}
		return nil
	})
}

func (s *managementState) handleInbounds(w http.ResponseWriter, r *http.Request) {
	_ = s.withMutation(func(mutation managementstate.Mutation) error {
		switch r.Method {
		case http.MethodGet:
			writeJSON(w, redactInboundList(mutation.Inbounds()))
		case http.MethodPost:
			var inbound Inbound
			if !decodeJSONRequest(w, r, &inbound) {
				return nil
			}
			inbound, autoErr := autofillInbound(inbound)
			if autoErr != nil {
				s.logUserAction(r, "create_inbound", inbound.Name, false, "provisioning failed")
				writeError(w, autoErr.Error(), http.StatusInternalServerError)
				return nil
			}
			created, candidate, err := inbounds.NewInboundCatalog(s.inbounds).Create(inbound)
			if err != nil {
				s.logUserAction(r, "create_inbound", inbound.Name, false, "")
				writeInboundManagementError(w, err)
				return nil
			}
			if validation, ok := s.enforceValidationLocked(r.Context(), s.settings, candidate.List(), s.warp); !ok {
				s.logUserAction(r, "create_inbound", inbound.Name, false, "live validation failed")
				writeValidationFailure(w, validation)
				return nil
			}
			created, err = mutation.CreateInbound(created)
			s.logUserAction(r, "create_inbound", inbound.Name, err == nil, "")
			if err != nil {
				writeInboundManagementError(w, err)
				return nil
			}
			actor, _ := r.Context().Value(contextKeyUsername).(string)
			outcome := s.autoApplyResultLocked(r, actor)
			s.writeMutationResponse(w, http.StatusCreated, redactInbound(created), outcome)
		default:
			methodNotAllowed(w, http.MethodGet, http.MethodPost)
		}
		return nil
	})
}

// autofillInbound delegates one-click provisioning to the inbound's protocol plugin.
func autofillInbound(inbound Inbound) (Inbound, error) {
	p, ok := protocols.NewRegistry().Get(inbound.Protocol)
	if !ok {
		return inbound, nil
	}
	ui, ok := protocols.AsUIProvider(p)
	if !ok {
		return inbound, nil
	}
	filled, err := ui.Autofill(inbound)
	if err != nil {
		return inbound, err
	}
	// Drop protocolFields keys that are not part of the protocol's inbound
	// schema: stale keys from a previously selected protocol or raw-API junk
	// otherwise persist and can reach renderers/links (audit #48/#98). Only
	// the fields the selected protocol declares are kept.
	filled.ProtocolFields = sanitizeInboundProtocolFields(p.Protocol(), filled.ProtocolFields)
	return filled, nil
}

// sanitizeInboundProtocolFields keeps only schema-declared inbound field keys
// for the given protocol, preserving the rest of the map untouched.
func sanitizeInboundProtocolFields(protocol string, fields map[string]any) map[string]any {
	if len(fields) == 0 {
		return fields
	}
	allowed := map[string]bool{}
	if plugin, ok := protocols.NewRegistry().Get(protocol); ok {
		if ui, ok := protocols.AsUIProvider(plugin); ok {
			for _, f := range ui.InboundFieldSchema() {
				allowed[f.Key] = true
			}
		}
	}
	clean := make(map[string]any, len(fields))
	for key, value := range fields {
		if allowed[key] {
			clean[key] = value
		}
	}
	return clean
}

// handleProtocolRoom returns a handler that backs the panel's "Generate" room
// button for the given protocol. It returns a fresh auto-generated room id for
// providers that support it, and refuses (400) for providers that require a
// manually created room.
func (s *managementState) handleProtocolRoom(protocol string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			methodNotAllowed(w, http.MethodPost)
			return
		}
		var req struct {
			Provider string `json:"provider"`
		}
		if !decodeJSONRequest(w, r, &req) {
			return
		}
		if req.Provider == "" {
			req.Provider = "jitsi"
		}

		p, ok := protocols.NewRegistry().Get(protocol)
		if !ok {
			writeNotFound(w)
			return
		}
		gen, ok := protocols.AsRoomGenerator(p)
		if !ok {
			writeNotFound(w)
			return
		}
		room, err := gen.GenerateRoom(req.Provider)
		if err != nil {
			writeError(w, err.Error(), http.StatusBadRequest)
			return
		}
		writeJSON(w, map[string]any{"provider": req.Provider, "roomID": room})
	}
}

func (s *managementState) handleInboundByName(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimPrefix(r.URL.Path, "/api/inbounds/")
	if name == "" || strings.Contains(name, "/") || !inbounds.IsSafeName(name) {
		writeNotFound(w)
		return
	}
	_ = s.withMutation(func(mutation managementstate.Mutation) error {
		inbound, ok := mutation.Inbound(name)
		if !ok {
			writeNotFound(w)
			return nil
		}
		switch r.Method {
		case http.MethodGet:
			writeJSON(w, redactInbound(inbound))
		case http.MethodPut:
			var update Inbound
			if !decodeJSONRequest(w, r, &update) {
				return nil
			}
			// The panel echoes the redacted GET representation on save; restore
			// the stored secrets before validation and persistence so that an
			// untouched password field is never written back as "[REDACTED]".
			update = preserveRedactedInbound(update, inbound)
			// Protocol plugins own one-click provisioning; running it on update
			// promotes ProtocolFields credentials (for example mieru/olcrtc
			// password) into the canonical flat fields the renderers consume.
			update, autoErr := autofillInbound(update)
			if autoErr != nil {
				s.logUserAction(r, "update_inbound", name, false, "provisioning failed")
				writeError(w, autoErr.Error(), http.StatusInternalServerError)
				return nil
			}
			updated, candidate, err := inbounds.NewInboundCatalog(s.inbounds).Update(name, update)
			if err != nil {
				s.logUserAction(r, "update_inbound", name, false, "")
				writeInboundManagementError(w, err)
				return nil
			}
			if validation, ok := s.enforceValidationLocked(r.Context(), s.settings, candidate.List(), s.warp); !ok {
				s.logUserAction(r, "update_inbound", name, false, "live validation failed")
				writeValidationFailure(w, validation)
				return nil
			}
			updated, err = mutation.UpdateInbound(name, updated)
			s.logUserAction(r, "update_inbound", name, err == nil, "")
			if err != nil {
				writeInboundManagementError(w, err)
				return nil
			}
			actor, _ := r.Context().Value(contextKeyUsername).(string)
			outcome := s.autoApplyResultLocked(r, actor)
			s.writeMutationResponse(w, http.StatusOK, redactInbound(updated), outcome)
		case http.MethodDelete:
			if s.clientRepo != nil {
				count, countErr := s.clientRepo.CountBindingsForInbound(name)
				if countErr != nil {
					writeError(w, "failed to check inbound bindings", http.StatusInternalServerError)
					return nil
				}
				if count > 0 {
					writeError(w, "inbound is referenced by client bindings", http.StatusConflict)
					return nil
				}
			}
			err := mutation.DeleteInbound(name)
			s.logUserAction(r, "delete_inbound", name, err == nil, "")
			if err != nil {
				writeInboundManagementError(w, err)
				return nil
			}
			actor, _ := r.Context().Value(contextKeyUsername).(string)
			outcome := s.autoApplyResultLocked(r, actor)
			s.writeMutationResponse(w, http.StatusOK, map[string]string{"name": name}, outcome)
		default:
			methodNotAllowed(w, http.MethodGet, http.MethodPut, http.MethodDelete)
		}
		return nil
	})
}

// isOlcrtcKey reports whether s is a 64-char lowercase hex string.
func isOlcrtcKey(s string) bool {
	if len(s) != 64 {
		return false
	}
	for _, c := range s {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			return false
		}
	}
	return true
}

func (s *managementState) handleProtocols(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, protocols.NewRegistry().ProtocolInfos())
}

func writeInboundManagementError(w http.ResponseWriter, err error) {
	switch err {
	case inbounds.ErrInboundInvalid:
		writeError(w, "name must contain only letters, digits, underscore, or hyphen; protocol, transport, and positive port are required", http.StatusBadRequest)
	case inbounds.ErrInboundDuplicateName:
		writeError(w, "inbound name already exists", http.StatusConflict)
	case inbounds.ErrInboundDuplicateTransportPort:
		writeError(w, "inbound transport/port already exists", http.StatusConflict)
	case inbounds.ErrInboundUnsupportedProtocolTransport:
		writeError(w, "unsupported inbound protocol/transport", http.StatusBadRequest)
	case inbounds.ErrInboundNotFound:
		writeNotFound(w)
	default:
		writeError(w, err.Error(), http.StatusInternalServerError)
	}
}
