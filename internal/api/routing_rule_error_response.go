package api

import "net/http"

type RoutingRuleErrorResponse struct{}

func NewRoutingRuleErrorResponse() RoutingRuleErrorResponse { return RoutingRuleErrorResponse{} }

func (RoutingRuleErrorResponse) Write(w http.ResponseWriter, err error) {
	switch err {
	case ErrRoutingRuleInvalid:
		writeError(w, "name, match, and outbound are required", http.StatusBadRequest)
	case ErrRoutingRuleDuplicateName:
		writeError(w, "routing rule name already exists", http.StatusConflict)
	case ErrRoutingRuleNotFound:
		writeNotFound(w)
	default:
		writeError(w, err.Error(), http.StatusInternalServerError)
	}
}

func writeRoutingRuleManagementError(w http.ResponseWriter, err error) {
	NewRoutingRuleErrorResponse().Write(w, err)
}
