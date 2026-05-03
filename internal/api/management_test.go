package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDownloadRouteDatReturnsBodyOnSuccess(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("route-dat-content"))
	}))
	defer server.Close()

	body, err := downloadRouteDat(server.URL)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(body) != "route-dat-content" {
		t.Fatalf("expected route-dat-content, got %q", string(body))
	}
}

func TestDownloadRouteDatReturnsErrorOnNon2xxStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	_, err := downloadRouteDat(server.URL)
	if err == nil {
		t.Fatal("expected error for 404, got nil")
	}
	if !strings.Contains(err.Error(), "404") && !strings.Contains(err.Error(), "Not Found") {
		t.Fatalf("error should mention status: %v", err)
	}
}

func TestDownloadRouteDatReturnsErrorOnInvalidURL(t *testing.T) {
	_, err := downloadRouteDat("://invalid-url")
	if err == nil {
		t.Fatal("expected error for invalid URL, got nil")
	}
}

func TestStackAllowsProtocolRejectsCrossStackProtocols(t *testing.T) {
	tests := []struct {
		stack    string
		protocol string
		want     bool
	}{
		{"naive", "naiveproxy", true},
		{"naive", "hysteria2", false},
		{"hysteria2", "hysteria2", true},
		{"hysteria2", "naiveproxy", false},
		{"both", "naiveproxy", true},
		{"both", "hysteria2", true},
		{"unknown", "naiveproxy", true},
		{"unknown", "hysteria2", true},
	}
	for _, tt := range tests {
		t.Run(tt.stack+"/"+tt.protocol, func(t *testing.T) {
			got := stackAllowsProtocol(tt.stack, tt.protocol)
			if got != tt.want {
				t.Fatalf("stackAllowsProtocol(%q, %q) = %v, want %v", tt.stack, tt.protocol, got, tt.want)
			}
		})
	}
}

func TestApplyHistoryStageReturnsCorrectStage(t *testing.T) {
	tests := []struct {
		name     string
		response ApplyResponse
		want     string
	}{
		{
			name:     "rollback stage",
			response: ApplyResponse{RolledBack: true},
			want:     "rollback",
		},
		{
			name:     "services stage supersedes live",
			response: ApplyResponse{ServicesApplied: true, LiveApplied: true},
			want:     "services",
		},
		{
			name:     "live stage",
			response: ApplyResponse{LiveApplied: true},
			want:     "live",
		},
		{
			name:     "staged fallback",
			response: ApplyResponse{},
			want:     "staged",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := applyHistoryStage(tt.response)
			if got != tt.want {
				t.Fatalf("applyHistoryStage() = %q, want %q", got, tt.want)
			}
		})
	}
}
