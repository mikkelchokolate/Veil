package api

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/veil-panel/veil/internal/routing"
)

func TestRoutingRuleErrorResponseMapsKnownErrors(t *testing.T) {
	cases := []struct {
		err  error
		code int
		body string
	}{
		{routing.ErrRoutingRuleInvalid, http.StatusBadRequest, "name, match, and outbound are required"},
		{routing.ErrRoutingRuleDuplicateName, http.StatusConflict, "routing rule name already exists"},
		{routing.ErrRoutingRuleNotFound, http.StatusNotFound, "not found"},
	}
	for _, tc := range cases {
		w := httptest.NewRecorder()
		NewRoutingRuleErrorResponse().Write(w, tc.err)
		if w.Code != tc.code || !strings.Contains(w.Body.String(), tc.body) {
			t.Fatalf("err=%v code=%d body=%s", tc.err, w.Code, w.Body.String())
		}
	}
}

func TestRoutingRuleErrorResponseMapsUnknownErrorToInternalServerError(t *testing.T) {
	w := httptest.NewRecorder()
	NewRoutingRuleErrorResponse().Write(w, errors.New("boom"))
	if w.Code != http.StatusInternalServerError || !strings.Contains(w.Body.String(), "boom") {
		t.Fatalf("code=%d body=%s", w.Code, w.Body.String())
	}
}
