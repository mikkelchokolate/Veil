package warp

import (
	"context"
	"encoding/base64"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

func TestGenerateKeypairProducesDistinctValidKeys(t *testing.T) {
	priv, pub, err := GenerateKeypair()
	if err != nil {
		t.Fatalf("GenerateKeypair: %v", err)
	}
	for name, key := range map[string]string{"private": priv, "public": pub} {
		raw, err := base64.StdEncoding.DecodeString(key)
		if err != nil {
			t.Fatalf("%s key not base64: %v", name, err)
		}
		if len(raw) != 32 {
			t.Fatalf("%s key must be 32 bytes, got %d", name, len(raw))
		}
	}
	if priv == pub {
		t.Fatal("private and public keys must differ")
	}
	// Two calls must not collide.
	priv2, _, _ := GenerateKeypair()
	if priv == priv2 {
		t.Fatal("successive private keys must differ")
	}
}

func TestRegistrarRegisterParsesCloudflareResponse(t *testing.T) {
	var gotKey string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || !strings.HasSuffix(r.URL.Path, "/reg") {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		if r.Header.Get("CF-Client-Version") == "" {
			t.Fatal("missing CF-Client-Version header")
		}
		body, _ := io.ReadAll(r.Body)
		if !strings.Contains(string(body), `"key"`) {
			t.Fatalf("request body missing key: %s", body)
		}
		gotKey = string(body)
		w.Write([]byte(`{
			"id":"device-123","token":"tok-456",
			"account":{"license":"LICENSE-KEY"},
			"config":{
				"client_id":"t0o+",
				"interface":{"addresses":{"v4":"172.16.0.2","v6":"2606:4700:110:8cdc::1"}},
				"peers":[{"public_key":"PEERPUBKEY=","endpoint":{"host":"engage.cloudflareclient.com:2408"}}]
			}
		}`))
	}))
	defer server.Close()

	reg, err := (&Registrar{BaseURL: server.URL, Client: server.Client()}).Register(context.Background())
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	if gotKey == "" {
		t.Fatal("registrar did not send a public key")
	}
	if reg.PeerPublicKey != "PEERPUBKEY=" {
		t.Fatalf("peer public key = %q", reg.PeerPublicKey)
	}
	if reg.Endpoint != "engage.cloudflareclient.com:2408" {
		t.Fatalf("endpoint = %q", reg.Endpoint)
	}
	if reg.LocalAddress != "172.16.0.2/32,2606:4700:110:8cdc::1/128" {
		t.Fatalf("local address = %q", reg.LocalAddress)
	}
	if !reflect.DeepEqual(reg.Reserved, []int{183, 74, 62}) {
		t.Fatalf("reserved = %v, want [183 74 62]", reg.Reserved)
	}
	if reg.License != "LICENSE-KEY" || reg.DeviceID != "device-123" || reg.AccessToken != "tok-456" {
		t.Fatalf("account fields = %+v", reg)
	}
	if priv, err := base64.StdEncoding.DecodeString(reg.PrivateKey); err != nil || len(priv) != 32 {
		t.Fatalf("private key invalid: %q err=%v", reg.PrivateKey, err)
	}
}

func TestRegistrarRegisterErrorsOnNon2xx(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte(`{"errors":[{"message":"denied"}]}`))
	}))
	defer server.Close()
	_, err := (&Registrar{BaseURL: server.URL, Client: server.Client()}).Register(context.Background())
	if err == nil {
		t.Fatal("expected error on non-2xx registration response")
	}
}

func TestRegistrarRegisterErrorsOnIncompleteResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"id":"x","config":{"peers":[]}}`))
	}))
	defer server.Close()
	_, err := (&Registrar{BaseURL: server.URL, Client: server.Client()}).Register(context.Background())
	if err == nil {
		t.Fatal("expected error on incomplete registration response")
	}
}
