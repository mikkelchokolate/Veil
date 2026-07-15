package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDecodeJSONRequestAcceptsMediaTypeParameters(t *testing.T) {
	request := httptest.NewRequest(http.MethodPost, "/api/test", strings.NewReader(`{"value":"ok"}`))
	request.Header.Set("Content-Type", "application/json; charset=utf-8")
	response := httptest.NewRecorder()
	var payload struct {
		Value string `json:"value"`
	}
	if !decodeJSONRequest(response, request, &payload) {
		t.Fatalf("decode failed: status=%d body=%s", response.Code, response.Body.String())
	}
	if payload.Value != "ok" {
		t.Fatalf("payload = %+v", payload)
	}
}

func TestValidateEmptyJSONBodyAcceptsMediaTypeParameters(t *testing.T) {
	for _, body := range []string{"", "{}"} {
		request := httptest.NewRequest(http.MethodPost, "/api/test", strings.NewReader(body))
		request.Header.Set("Content-Type", "application/json; charset=UTF-8")
		if err := validateEmptyJSONBody(request); err != nil {
			t.Fatalf("body %q: %v", body, err)
		}
	}
}

func TestEmptyJSONBodyErrorsUseCanonicalHTTPStatuses(t *testing.T) {
	t.Run("unsupported media type", func(t *testing.T) {
		request := httptest.NewRequest(http.MethodPost, "/api/test", strings.NewReader("{}"))
		request.Header.Set("Content-Type", "text/plain")
		err := validateEmptyJSONBody(request)
		if err == nil {
			t.Fatal("expected content-type error")
		}
		response := httptest.NewRecorder()
		writeError(response, err.Error(), http.StatusBadRequest)
		if response.Code != http.StatusUnsupportedMediaType {
			t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
		}
	})

	t.Run("oversized body", func(t *testing.T) {
		request := httptest.NewRequest(http.MethodPost, "/api/test", strings.NewReader(strings.Repeat("x", int(maxJSONBodyBytes)+1)))
		request.Header.Set("Content-Type", "application/json")
		err := validateEmptyJSONBody(request)
		if err == nil {
			t.Fatal("expected body-size error")
		}
		response := httptest.NewRecorder()
		writeError(response, err.Error(), http.StatusBadRequest)
		if response.Code != http.StatusRequestEntityTooLarge {
			t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
		}
	})
}

func TestJSONMediaTypeRejectsMalformedAndNonJSONValues(t *testing.T) {
	for _, value := range []string{"text/json", "application/json; charset", "application/xml"} {
		if isJSONMediaType(value) {
			t.Fatalf("isJSONMediaType(%q) = true", value)
		}
	}
}
