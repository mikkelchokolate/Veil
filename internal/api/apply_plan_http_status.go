package api

import "net/http"

type ApplyPlanHTTPStatus struct{}

func NewApplyPlanHTTPStatus() ApplyPlanHTTPStatus { return ApplyPlanHTTPStatus{} }

func (ApplyPlanHTTPStatus) Status(plan ApplyPlanResponse) int {
	if !plan.Valid {
		return http.StatusBadRequest
	}
	return http.StatusOK
}
