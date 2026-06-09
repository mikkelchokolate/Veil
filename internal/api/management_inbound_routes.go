package api

import (
	"net/http"
	"strings"

	"github.com/mikkelchokolate/Veil/internal/inbounds"
	"github.com/mikkelchokolate/Veil/internal/managementstate"
	"github.com/mikkelchokolate/Veil/internal/protocols"
	veilsettings "github.com/mikkelchokolate/Veil/internal/settings"
)

func (s *managementState) handleSettings(w http.ResponseWriter, r *http.Request) {
	_ = s.withMutation(func(mutation managementstate.Mutation) error {
		switch r.Method {
		case http.MethodGet:
			writeJSON(w, mutation.Settings())
		case http.MethodPut:
			var settings Settings
			if !decodeJSONRequest(w, r, &settings) {
				return nil
			}
			candidate := settings
			if err := veilsettings.NewSettingsValidation().NormalizeAndValidate(&candidate, s.settings); err != nil {
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
			writeJSON(w, updated)
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
			writeJSON(w, mutation.Inbounds())
		case http.MethodPost:
			var inbound Inbound
			if !decodeJSONRequest(w, r, &inbound) {
				return nil
			}
			inbound, autoErr := autofillOlcrtcInbound(inbound)
			if autoErr != nil {
				s.logUserAction(r, "create_inbound", inbound.Name, false, "olcrtc provisioning failed")
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
			writeJSONStatus(w, http.StatusCreated, created)
		default:
			methodNotAllowed(w, http.MethodGet, http.MethodPost)
		}
		return nil
	})
}

// autofillOlcrtcInbound makes olcRTC a one-click protocol: the operator only
// picks "olcrtc" and we provision a working config. olcRTC disguises traffic as
// a WebRTC "meet" call, so it needs a provider, a room URL both ends share, a
// channel transport, and a single shared crypto key. For providers that support
// auto-rooms (Jitsi rooms are created on join) we generate the room; for
// providers that need a manually created room we leave it for the operator. The
// 64-hex shared key lives in the inbound password slot.
func autofillOlcrtcInbound(inbound Inbound) (Inbound, error) {
	if inbound.Protocol != "olcrtc" {
		return inbound, nil
	}
	if inbound.OlcrtcAuth == "" {
		inbound.OlcrtcAuth = "jitsi"
	}
	if inbound.OlcrtcTransport == "" {
		inbound.OlcrtcTransport = "datachannel"
	}
	if inbound.OlcrtcRoomID == "" && protocols.OlcrtcProviderSupportsAutoRoom(inbound.OlcrtcAuth) {
		room, err := protocols.GenerateOlcrtcRoom(inbound.OlcrtcAuth)
		if err != nil {
			return inbound, err
		}
		inbound.OlcrtcRoomID = room
	}
	if !isOlcrtcKey(inbound.Password) {
		key, err := generateRandomHex(64)
		if err != nil {
			return inbound, err
		}
		inbound.Password = key
	}
	return inbound, nil
}

// handleOlcrtcRoom backs the panel's "Generate" room button. It returns a fresh
// auto-generated room id for providers that support it, and refuses (400) for
// providers that require a manually created room — the same rule the panel uses
// to enable/disable the button, enforced server-side.
func (s *managementState) handleOlcrtcRoom(w http.ResponseWriter, r *http.Request) {
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
	room, err := protocols.GenerateOlcrtcRoom(req.Provider)
	if err != nil {
		writeError(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, map[string]any{"provider": req.Provider, "roomID": room})
}

// isOlcrtcKey reports whether s is a 64-char lowercase hex string, the crypto
// key format olcRTC requires.
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

func (s *managementState) handleInboundByName(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimPrefix(r.URL.Path, "/api/inbounds/")
	if name == "" || strings.Contains(name, "/") {
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
			writeJSON(w, inbound)
		case http.MethodPut:
			var update Inbound
			if !decodeJSONRequest(w, r, &update) {
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
			writeJSON(w, updated)
		case http.MethodDelete:
			err := mutation.DeleteInbound(name)
			s.logUserAction(r, "delete_inbound", name, err == nil, "")
			if err != nil {
				writeInboundManagementError(w, err)
				return nil
			}
			w.WriteHeader(http.StatusNoContent)
		default:
			methodNotAllowed(w, http.MethodGet, http.MethodPut, http.MethodDelete)
		}
		return nil
	})
}

func writeInboundManagementError(w http.ResponseWriter, err error) {
	switch err {
	case inbounds.ErrInboundInvalid:
		writeError(w, "name, protocol, transport, and positive port are required", http.StatusBadRequest)
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
