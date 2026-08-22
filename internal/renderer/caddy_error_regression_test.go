package renderer

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/mikkelchokolate/Veil/internal/bindregistry"
	"github.com/mikkelchokolate/Veil/internal/caddyassembly"
	"github.com/mikkelchokolate/Veil/internal/caddycapabilities"
)

func TestPanelCaddyServerHasBoundedDiagnosticErrorResponse(t *testing.T) {
	server, err := renderServer(
		bindregistry.BindKey{Network: bindregistry.ListenTCP, Address: "0.0.0.0", Port: 443},
		caddyassembly.CaddyBindOwner{Kind: caddyassembly.CaddyOwnerPanel, Domain: "panel.example", BackendPort: 8080, WebBasePath: "/secret"},
		caddycapabilities.CaddyCapabilities{},
	)
	if err != nil {
		t.Fatal(err)
	}
	errorsConfig, ok := server["errors"]
	if !ok {
		t.Fatal("panel Caddy server has no bounded error route")
	}
	body, err := json.Marshal(errorsConfig)
	if err != nil {
		t.Fatal(err)
	}
	if len(body) > 2048 || !strings.Contains(string(body), "panel backend unavailable") || !strings.Contains(string(body), "http.error.status_code") {
		t.Fatalf("error config=%s", body)
	}
}
