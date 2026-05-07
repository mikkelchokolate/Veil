package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHTTPStatusRecorderCapturesExplicitAndImplicitStatus(t *testing.T) {
	recorder := NewHTTPStatusRecorder(httptest.NewRecorder())
	recorder.WriteHeader(http.StatusCreated)
	if recorder.StatusCode() != http.StatusCreated {
		t.Fatalf("status = %d", recorder.StatusCode())
	}

	implicit := NewHTTPStatusRecorder(httptest.NewRecorder())
	_, _ = implicit.Write([]byte("ok"))
	if implicit.StatusCode() != http.StatusOK {
		t.Fatalf("implicit status = %d", implicit.StatusCode())
	}
}
