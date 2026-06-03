package api

import (
	"net/http"

	"github.com/mikkelchokolate/Veil/internal/clientaccess"
	"github.com/mikkelchokolate/Veil/internal/firewall"
)

var firewallStatusReader = func() (bool, error) {
	return firewall.NewStatusReader(nil).Active()
}

func (s *managementState) handleClientLinks(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w, http.MethodGet)
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	response, err := clientaccess.BuildClientLinks(s.settings, s.inbounds)
	if err != nil {
		writeError(w, err.Error(), http.StatusBadRequest)
		return
	}
	clientaccess.NewClientLinkDeliveryHeaders().Apply(w.Header())
	writeJSON(w, response)
}

func (s *managementState) handleClientLinksSubscription(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w, http.MethodGet)
		return
	}
	query := r.URL.Query()
	if err := clientaccess.ValidateClientSubscriptionQuery(query); err != nil {
		writeError(w, err.Error(), http.StatusBadRequest)
		return
	}
	format := query.Get("format")
	s.mu.Lock()
	defer s.mu.Unlock()
	response, err := clientaccess.BuildClientLinks(s.settings, s.inbounds)
	if err != nil {
		writeError(w, err.Error(), http.StatusBadRequest)
		return
	}
	subscription, err := clientaccess.BuildClientSubscription(response, format)
	if err != nil {
		writeError(w, err.Error(), http.StatusBadRequest)
		return
	}
	clientaccess.NewClientSubscriptionDeliveryHeaders(subscription).Apply(w.Header())
	_, _ = w.Write([]byte(subscription.Body))
}

func (s *managementState) handleFirewall(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		methodNotAllowed(w, http.MethodGet, http.MethodHead)
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	active, _ := firewallStatusReader()
	rules := firewall.BuildRuleResponses(s.settings, s.inbounds)
	if r.Method == http.MethodGet {
		writeJSON(w, map[string]any{
			"active": active,
			"rules":  rules,
		})
	} else {
		setJSONHeaders(w)
	}
}

func (s *managementState) handleApplyPlan(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w, http.MethodPost)
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	plan := NewManagementApplyContext(s).buildApplyPlanLocked()
	status := http.StatusOK
	if !plan.Valid {
		status = http.StatusBadRequest
	}
	writeJSONStatus(w, status, plan)
}

func (s *managementState) handleApplyHistory(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w, http.MethodGet)
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	history, err := s.applyHistoryLocked().Query(r.URL.Query())
	if err != nil {
		writeError(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, history)
}

func (s *managementState) handleApply(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w, http.MethodPost)
		return
	}
	var req ApplyRequest
	if !decodeJSONRequest(w, r, &req) {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	response, status, err := NewApplyWorkflow(NewManagementApplyContext(s)).RunLocked(req)
	s.logUserAction(r, "apply_configuration", "system", err == nil && status == http.StatusOK, "")
	if err != nil {
		writeError(w, err.Error(), status)
		return
	}
	if status != http.StatusOK {
		writeJSONStatus(w, status, response)
		return
	}
	writeJSON(w, response)
}
