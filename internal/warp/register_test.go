package warp

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"
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

func TestNewRegistrarReturnsUsableRegistrar(t *testing.T) {
	r := NewRegistrar()
	if r == nil {
		t.Fatal("NewRegistrar returned nil")
	}
	if r.client() == nil {
		t.Fatal("default client is nil")
	}
}

func TestRegistrarDefaultClientTimeout(t *testing.T) {
	r := NewRegistrar()
	c := r.client()
	if c.Timeout != 15*time.Second {
		t.Fatalf("default timeout = %v, want 15s", c.Timeout)
	}
}

func TestRegistrarUsesProvidedClient(t *testing.T) {
	custom := &http.Client{Timeout: 1 * time.Second}
	r := &Registrar{Client: custom}
	if r.client() != custom {
		t.Fatal("registrar did not use provided client")
	}
}

func TestRegistrarBaseURLTrimsTrailingSlash(t *testing.T) {
	r := &Registrar{BaseURL: "https://example.com/"}
	if got := r.baseURL(); got != "https://example.com" {
		t.Fatalf("baseURL = %q, want %q", got, "https://example.com")
	}
}

func TestRegistrarBaseURLDefault(t *testing.T) {
	r := NewRegistrar()
	if got := r.baseURL(); got != defaultRegBaseURL {
		t.Fatalf("baseURL = %q, want %q", got, defaultRegBaseURL)
	}
}

func TestRegistrarBaseURLIgnoresWhitespace(t *testing.T) {
	r := &Registrar{BaseURL: "   "}
	if got := r.baseURL(); got != defaultRegBaseURL {
		t.Fatalf("baseURL = %q, want default", got)
	}
}

func TestRegistrarClientVerDefault(t *testing.T) {
	r := NewRegistrar()
	if got := r.clientVer(); got != defaultClientVer {
		t.Fatalf("clientVer = %q, want %q", got, defaultClientVer)
	}
}

func TestRegistrarClientVerOverride(t *testing.T) {
	r := &Registrar{ClientVer: "custom-version"}
	if got := r.clientVer(); got != "custom-version" {
		t.Fatalf("clientVer = %q, want custom-version", got)
	}
}

func TestRegistrarHostnameOverride(t *testing.T) {
	r := &Registrar{Hostname: "myhost"}
	if got := r.hostname(); got != "myhost" {
		t.Fatalf("hostname = %q, want myhost", got)
	}
}

func TestRegistrarHostnameDefaultNonEmpty(t *testing.T) {
	r := NewRegistrar()
	got := r.hostname()
	if got == "" {
		t.Fatal("default hostname is empty")
	}
}

func TestRegistrarRegisterUsesDefaultEndpoint(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{
			"id":"device-123","token":"tok-456",
			"account":{"license":"LICENSE-KEY"},
			"config":{
				"client_id":"t0o+",
				"interface":{"addresses":{"v4":"172.16.0.2","v6":"2606:4700:110:8cdc::1"}},
				"peers":[{"public_key":"PEERPUBKEY=","endpoint":{"host":""}}]
			}
		}`))
	}))
	defer server.Close()

	reg, err := (&Registrar{BaseURL: server.URL, Client: server.Client()}).Register(context.Background())
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	if reg.Endpoint != defaultEndpoint {
		t.Fatalf("endpoint = %q, want %q", reg.Endpoint, defaultEndpoint)
	}
}

func TestRegistrarRegisterOnlyV4Address(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{
			"id":"device-123","token":"tok-456",
			"account":{"license":"LICENSE-KEY"},
			"config":{
				"client_id":"t0o+",
				"interface":{"addresses":{"v4":"172.16.0.2"}},
				"peers":[{"public_key":"PEERPUBKEY=","endpoint":{"host":"engage.cloudflareclient.com:2408"}}]
			}
		}`))
	}))
	defer server.Close()

	reg, err := (&Registrar{BaseURL: server.URL, Client: server.Client()}).Register(context.Background())
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	if reg.LocalAddress != "172.16.0.2/32" {
		t.Fatalf("local address = %q", reg.LocalAddress)
	}
}

func TestRegistrarRegisterOnlyV6Address(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{
			"id":"device-123","token":"tok-456",
			"account":{"license":"LICENSE-KEY"},
			"config":{
				"client_id":"t0o+",
				"interface":{"addresses":{"v6":"2606:4700:110:8cdc::1"}},
				"peers":[{"public_key":"PEERPUBKEY=","endpoint":{"host":"engage.cloudflareclient.com:2408"}}]
			}
		}`))
	}))
	defer server.Close()

	reg, err := (&Registrar{BaseURL: server.URL, Client: server.Client()}).Register(context.Background())
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	if reg.LocalAddress != "2606:4700:110:8cdc::1/128" {
		t.Fatalf("local address = %q", reg.LocalAddress)
	}
}

func TestRegistrarRegisterMissingAddresses(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{
			"id":"device-123","token":"tok-456",
			"account":{"license":"LICENSE-KEY"},
			"config":{
				"client_id":"t0o+",
				"interface":{"addresses":{}},
				"peers":[{"public_key":"PEERPUBKEY=","endpoint":{"host":"engage.cloudflareclient.com:2408"}}]
			}
		}`))
	}))
	defer server.Close()

	_, err := (&Registrar{BaseURL: server.URL, Client: server.Client()}).Register(context.Background())
	if err == nil {
		t.Fatal("expected error when addresses are missing")
	}
	if !strings.Contains(err.Error(), "no interface addresses") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRegistrarRegisterMissingID(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{
			"token":"tok-456",
			"account":{"license":"LICENSE-KEY"},
			"config":{
				"client_id":"t0o+",
				"interface":{"addresses":{"v4":"172.16.0.2"}},
				"peers":[{"public_key":"PEERPUBKEY=","endpoint":{"host":"engage.cloudflareclient.com:2408"}}]
			}
		}`))
	}))
	defer server.Close()

	_, err := (&Registrar{BaseURL: server.URL, Client: server.Client()}).Register(context.Background())
	if err == nil {
		t.Fatal("expected error when id is missing")
	}
	if !strings.Contains(err.Error(), "incomplete registration response") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRegistrarRegisterMissingPeerKey(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{
			"id":"device-123","token":"tok-456",
			"account":{"license":"LICENSE-KEY"},
			"config":{
				"client_id":"t0o+",
				"interface":{"addresses":{"v4":"172.16.0.2"}},
				"peers":[{"public_key":"","endpoint":{"host":"engage.cloudflareclient.com:2408"}}]
			}
		}`))
	}))
	defer server.Close()

	_, err := (&Registrar{BaseURL: server.URL, Client: server.Client()}).Register(context.Background())
	if err == nil {
		t.Fatal("expected error when peer public key is missing")
	}
	if !strings.Contains(err.Error(), "incomplete registration response") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRegistrarRegisterInvalidClientID(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{
			"id":"device-123","token":"tok-456",
			"account":{"license":"LICENSE-KEY"},
			"config":{
				"client_id":"not-valid-base64!!!",
				"interface":{"addresses":{"v4":"172.16.0.2"}},
				"peers":[{"public_key":"PEERPUBKEY=","endpoint":{"host":"engage.cloudflareclient.com:2408"}}]
			}
		}`))
	}))
	defer server.Close()

	_, err := (&Registrar{BaseURL: server.URL, Client: server.Client()}).Register(context.Background())
	if err == nil {
		t.Fatal("expected error on invalid client_id")
	}
	if !strings.Contains(err.Error(), "decode client_id") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRegistrarRegisterContextCancelled(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// delay long enough for cancellation to win the race
		time.Sleep(50 * time.Millisecond)
		w.Write([]byte(`{}`))
	}))
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Millisecond)
	defer cancel()
	_, err := (&Registrar{BaseURL: server.URL, Client: server.Client()}).Register(ctx)
	if err == nil {
		t.Fatal("expected error on cancelled context")
	}
}

func TestRegistrarRegisterRequestRoundTripError(t *testing.T) {
	r := &Registrar{
		BaseURL: "http://example.com",
		Client:  &http.Client{Transport: errorRoundTripper{}},
	}
	_, err := r.Register(context.Background())
	if err == nil {
		t.Fatal("expected error when round trip fails")
	}
	if !strings.Contains(err.Error(), "warp: register request") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRegistrarRegisterBodyReadError(t *testing.T) {
	r := &Registrar{
		BaseURL: "http://example.com",
		Client: &http.Client{Transport: fixedResponseRoundTripper{resp: &http.Response{
			StatusCode: http.StatusOK,
			Body:       &errorReadCloser{err: errors.New("read failed")},
			Header:     make(http.Header),
		}}},
	}
	_, err := r.Register(context.Background())
	if err == nil {
		t.Fatal("expected error when body read fails")
	}
}

func TestRegistrarRegisterMalformedJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{not json`))
	}))
	defer server.Close()

	_, err := (&Registrar{BaseURL: server.URL, Client: server.Client()}).Register(context.Background())
	if err == nil {
		t.Fatal("expected error on malformed JSON")
	}
	if !strings.Contains(err.Error(), "decode registration") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestDecodeReservedEmpty(t *testing.T) {
	got, err := decodeReserved("")
	if err != nil {
		t.Fatalf("decodeReserved(\"\"): %v", err)
	}
	if got != nil {
		t.Fatalf("expected nil, got %v", got)
	}
}

func TestDecodeReservedInvalidBase64(t *testing.T) {
	_, err := decodeReserved("!!!")
	if err == nil {
		t.Fatal("expected error on invalid base64")
	}
}

func TestDecodeReservedMultipleBytes(t *testing.T) {
	// base64 of bytes {0, 127, 255}
	got, err := decodeReserved(base64.StdEncoding.EncodeToString([]byte{0, 127, 255}))
	if err != nil {
		t.Fatalf("decodeReserved: %v", err)
	}
	want := []int{0, 127, 255}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("reserved = %v, want %v", got, want)
	}
}

func TestDecodeReservedSingleByte(t *testing.T) {
	got, err := decodeReserved(base64.StdEncoding.EncodeToString([]byte{42}))
	if err != nil {
		t.Fatalf("decodeReserved: %v", err)
	}
	want := []int{42}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("reserved = %v, want %v", got, want)
	}
}

func TestRegistrarRegisterUsesHostnameOverride(t *testing.T) {
	var gotName string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		const prefix = `"name":"`
		start := strings.Index(string(body), prefix)
		if start != -1 {
			start += len(prefix)
			end := strings.Index(string(body)[start:], `"`)
			if end != -1 {
				gotName = string(body)[start : start+end]
			}
		}
		w.Write([]byte(`{
			"id":"device-123","token":"tok-456",
			"account":{"license":"LICENSE-KEY"},
			"config":{
				"client_id":"t0o+",
				"interface":{"addresses":{"v4":"172.16.0.2"}},
				"peers":[{"public_key":"PEERPUBKEY=","endpoint":{"host":"engage.cloudflareclient.com:2408"}}]
			}
		}`))
	}))
	defer server.Close()

	_, err := (&Registrar{BaseURL: server.URL, Client: server.Client(), Hostname: "test-host"}).Register(context.Background())
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	if gotName != "test-host" {
		t.Fatalf("hostname in request = %q, want test-host", gotName)
	}
}

// errorRoundTripper always returns an error.
type errorRoundTripper struct{}

func (errorRoundTripper) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, errors.New("round trip failed")
}

// fixedResponseRoundTripper returns a fixed response.
type fixedResponseRoundTripper struct {
	resp *http.Response
}

func (rt fixedResponseRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	rt.resp.Request = req
	return rt.resp, nil
}

// errorReadCloser simulates a read error.
type errorReadCloser struct {
	err error
}

func (e *errorReadCloser) Read(p []byte) (int, error) { return 0, e.err }
func (e *errorReadCloser) Close() error               { return nil }

// compile-time interface checks.
var _ http.RoundTripper = errorRoundTripper{}
var _ http.RoundTripper = fixedResponseRoundTripper{}
var _ io.ReadCloser = (*errorReadCloser)(nil)

// avoid unused import linting for bytes.
var _ = bytes.NewReader

func TestGenerateKeypairReturnsErrorOnRandomReadFailure(t *testing.T) {
	orig := randReader
	randReader = &errorReader{err: errors.New("random read failed")}
	defer func() { randReader = orig }()

	_, _, err := GenerateKeypair()
	if err == nil {
		t.Fatal("expected error when random read fails")
	}
	if !strings.Contains(err.Error(), "generate private key") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRegistrarRegisterPropagatesKeypairError(t *testing.T) {
	orig := randReader
	randReader = &errorReader{err: errors.New("random read failed")}
	defer func() { randReader = orig }()

	r := &Registrar{BaseURL: "http://example.com", Client: &http.Client{Transport: errorRoundTripper{}}}
	_, err := r.Register(context.Background())
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "generate private key") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRegistrarRegisterErrorsOnInvalidBaseURL(t *testing.T) {
	orig := randReader
	randReader = &countingReader{limit: 32}
	defer func() { randReader = orig }()

	r := &Registrar{BaseURL: "http://[::1]:namedport"}
	_, err := r.Register(context.Background())
	if err == nil {
		t.Fatal("expected error on invalid base URL")
	}
}

func TestRegistrarHostnameDefaultFallsBackToVeil(t *testing.T) {
	orig := osHostname
	osHostname = func() (string, error) { return "", errors.New("no hostname") }
	defer func() { osHostname = orig }()

	r := NewRegistrar()
	if got := r.hostname(); got != "veil" {
		t.Fatalf("hostname = %q, want veil", got)
	}
}

func TestGenerateKeypairReturnsErrorOnX25519Failure(t *testing.T) {
	origRand := randReader
	origX := x25519
	randReader = &countingReader{limit: 32}
	x25519 = func(scalar, point []byte) ([]byte, error) {
		return nil, errors.New("x25519 failed")
	}
	defer func() {
		randReader = origRand
		x25519 = origX
	}()

	_, _, err := GenerateKeypair()
	if err == nil {
		t.Fatal("expected error when x25519 fails")
	}
	if !strings.Contains(err.Error(), "derive public key") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRegistrarRegisterPropagatesMarshalError(t *testing.T) {
	origRand := randReader
	origMarshal := jsonMarshal
	randReader = &countingReader{limit: 32}
	jsonMarshal = func(v any) ([]byte, error) {
		return nil, errors.New("marshal failed")
	}
	defer func() {
		randReader = origRand
		jsonMarshal = origMarshal
	}()

	r := &Registrar{BaseURL: "http://example.com"}
	_, err := r.Register(context.Background())
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "marshal failed") {
		t.Fatalf("unexpected error: %v", err)
	}
}

// errorReader returns an error after optionally allowing a limited number of bytes.
type errorReader struct {
	err   error
	reads int
	limit int
}

func (e *errorReader) Read(p []byte) (int, error) {
	if e.limit > 0 && e.reads < e.limit {
		e.reads++
		return 1, nil
	}
	return 0, e.err
}

// countingReader returns the requested bytes up to a limit, then errors.
type countingReader struct {
	read  int
	limit int
}

func (c *countingReader) Read(p []byte) (int, error) {
	remaining := c.limit - c.read
	if remaining <= 0 {
		return 0, errors.New("exhausted")
	}
	n := len(p)
	if n > remaining {
		n = remaining
	}
	for i := 0; i < n; i++ {
		p[i] = byte(i)
	}
	c.read += n
	return n, nil
}
