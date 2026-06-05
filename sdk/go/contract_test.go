package veilclient

import (
	"context"
	"net/http"
	"testing"
)

func TestGeneratedClientCoversAdminAndPreviewRoutes(t *testing.T) {
	rotate, err := NewPostApiAdminRotateKeyRequest("https://veil.example/base/", EmptyObject{})
	if err != nil {
		t.Fatal(err)
	}
	if rotate.Method != http.MethodPost || rotate.URL.Path != "/base/api/admin/rotate-key" {
		t.Fatalf("rotate request=%s %s", rotate.Method, rotate.URL.Path)
	}
	preview, err := NewPostApiProfilesRuRecommendedPreviewRequest(
		"https://veil.example/base/",
		RURecommendedPreviewRequest{Domain: "vpn.example.com", Email: "admin@example.com"},
	)
	if err != nil {
		t.Fatal(err)
	}
	if preview.Method != http.MethodPost || preview.URL.Path != "/base/api/profiles/ru-recommended/preview" {
		t.Fatalf("preview request=%s %s", preview.Method, preview.URL.Path)
	}
}

func TestGeneratedClientCoversSessionLocaleRoute(t *testing.T) {
	request, err := NewPostApiAuthLocaleRequest(
		"https://veil.example/base/",
		LocaleUpdateRequest{Locale: "ru"},
	)
	if err != nil {
		t.Fatal(err)
	}
	if request.Method != http.MethodPost || request.URL.Path != "/base/api/auth/locale" {
		t.Fatalf("locale request=%s %s", request.Method, request.URL.Path)
	}
}

func TestGeneratedClientSupportsTokenRequestEditor(t *testing.T) {
	client, err := NewClient("https://veil.example", WithRequestEditorFn(func(_ context.Context, request *http.Request) error {
		request.Header.Set("X-Veil-Token", "test-token")
		return nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	request, err := NewGetApiStatusRequest(client.Server)
	if err != nil {
		t.Fatal(err)
	}
	for _, editor := range client.RequestEditors {
		if err := editor(context.Background(), request); err != nil {
			t.Fatal(err)
		}
	}
	if request.Header.Get("X-Veil-Token") != "test-token" {
		t.Fatal("token request editor was not applied")
	}
}
