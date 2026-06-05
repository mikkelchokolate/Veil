package api

import (
	"net/http"
	"strings"

	"github.com/mikkelchokolate/Veil/internal/inbounds"
	"github.com/mikkelchokolate/Veil/internal/managementstate"
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
