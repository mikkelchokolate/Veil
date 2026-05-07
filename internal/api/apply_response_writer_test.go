package api

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestApplyResponseWriterWritesErrorStatus(t *testing.T) {
	w := httptest.NewRecorder()
	NewApplyResponseWriter().Write(w, ApplyResponse{}, http.StatusConflict, errors.New("boom"))
	if w.Code != http.StatusConflict || !strings.Contains(w.Body.String(), "boom") {
		t.Fatalf("code=%d body=%s", w.Code, w.Body.String())
	}
}

func TestApplyResponseWriterWritesStatusJSONWhenNotOK(t *testing.T) {
	w := httptest.NewRecorder()
	NewApplyResponseWriter().Write(w, ApplyResponse{Applied: false}, http.StatusBadRequest, nil)
	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "applied") {
		t.Fatalf("code=%d body=%s", w.Code, w.Body.String())
	}
}
