package api

import (
	"context"
	"net/http"

	"github.com/mikkelchokolate/Veil/internal/livevalidation"
	"github.com/mikkelchokolate/Veil/internal/model"
)

type validationRequest struct {
	Settings Settings   `json:"settings"`
	Inbounds []Inbound  `json:"inbounds"`
	Warp     WarpConfig `json:"warp"`
}

type validationErrorEnvelope struct {
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
	Issues []model.ValidationIssue `json:"issues"`
}

func (s *managementState) handleValidation(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w, http.MethodPost)
		return
	}
	var request validationRequest
	if !decodeJSONRequest(w, r, &request) {
		return
	}
	s.mu.Lock()
	response := s.validateConfigurationLocked(r.Context(), request.Settings, request.Inbounds, request.Warp)
	s.mu.Unlock()
	writeJSON(w, response)
}

func (s *managementState) validateConfigurationLocked(
	ctx context.Context,
	settings Settings,
	inbounds []Inbound,
	warp WarpConfig,
) livevalidation.Response {
	return s.configurationValidator.Validate(ctx, livevalidation.Request{
		Settings:        settings,
		Inbounds:        append([]Inbound(nil), inbounds...),
		CurrentInbounds: append([]Inbound(nil), s.inbounds...),
		Warp:            warp,
	})
}

func (s *managementState) enforceValidationLocked(
	ctx context.Context,
	settings Settings,
	inbounds []Inbound,
	warp WarpConfig,
) (livevalidation.Response, bool) {
	if !s.enforceConfigurationValidation {
		return livevalidation.Response{Valid: true, Issues: []model.ValidationIssue{}}, true
	}
	response := s.validateConfigurationLocked(ctx, settings, inbounds, warp)
	return response, response.Valid
}

func writeValidationFailure(w http.ResponseWriter, response livevalidation.Response) {
	payload := validationErrorEnvelope{Issues: response.Issues}
	payload.Error.Code = "validation_failed"
	payload.Error.Message = "configuration failed live validation"
	writeJSONStatus(w, http.StatusUnprocessableEntity, payload)
}
