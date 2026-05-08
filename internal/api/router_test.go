package api

import (
	"bytes"
	"crypto/sha256"
	"crypto/tls"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var _routerTestDeps_router_test = []any{
	bytes.Buffer{}, sha256.Sum256, tls.VersionTLS12, base64.StdEncoding, hex.EncodeToString, json.NewDecoder, errors.New, fmt.Sprintf, log.Printf, http.MethodGet, httptest.NewRecorder, os.ReadFile, filepath.Join, strings.Contains, testing.T{},
}

func assertInvalidSubscriptionFormat(t *testing.T, w *httptest.ResponseRecorder) {
	t.Helper()
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid subscription format, got %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "format must be base64 or raw") {
		t.Fatalf("unexpected invalid format error: %q", w.Body.String())
	}
}

func assertClientSubscriptionLines(t *testing.T, body string) {
	t.Helper()
	lines := strings.Split(strings.TrimSpace(body), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 subscription links, got %q", body)
	}
	if lines[0] != "https://veil:naive-secret@vpn.example.com:443" {
		t.Fatalf("unexpected first subscription link: %q", lines[0])
	}
	if !strings.HasPrefix(lines[1], "hysteria2://hy2-secret@vpn.example.com:443/") || !strings.Contains(lines[1], "sni=vpn.example.com") {
		t.Fatalf("unexpected second subscription link: %q", lines[1])
	}
}

func testSHA256Line(body string, name string) string {
	sum := sha256.Sum256([]byte(body))
	return hex.EncodeToString(sum[:]) + "  " + name + "\n"
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func stringSlicesEqual(left []string, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}

func writeRenderableManagementState(path string, stack string) error {
	return os.WriteFile(path, []byte(`{
		"settings":{
			"panelListen":"127.0.0.1:2096",
			"stack":"`+stack+`",
			"mode":"dev",
			"domain":"vpn.example.com",
			"email":"admin@example.com",
			"naiveUsername":"veil",
			"naivePassword":"naive-secret",
			"hysteria2Password":"hy2-secret",
			"masqueradeURL":"https://www.bing.com/",
			"fallbackRoot":"/var/lib/veil/www"
		},
		"inbounds":[
			{"name":"naive","protocol":"naiveproxy","transport":"tcp","port":443,"enabled":true},
			{"name":"hysteria2","protocol":"hysteria2","transport":"udp","port":443,"enabled":true}
		],
		"routingRules":[],
		"warp":{"enabled":false,"endpoint":"engage.cloudflareclient.com:2408"}
	}`), 0o600)
}

func writeRenderableMieruManagementState(path string) error {
	return os.WriteFile(path, []byte(`{
		"settings":{"panelListen":"127.0.0.1:2096","stack":"mieru","mode":"dev"},
		"inbounds":[{"name":"mieru","protocol":"mieru","transport":"tcp","port":443,"enabled":true,"password":"mieru-secret"}],
		"routingRules":[],
		"warp":{"enabled":false,"endpoint":"engage.cloudflareclient.com:2408"}
	}`), 0o600)
}
