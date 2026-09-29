package caddyassembly

import (
	"github.com/mikkelchokolate/Veil/internal/bindregistry"
	"github.com/mikkelchokolate/Veil/internal/model"
)

// PanelIPCertCaddyFronted reports whether the managed Caddy edge will own the
// public HTTP-01 port for the given configuration — i.e. the final render plan
// keeps a veil-caddy-owned listener on TCP :80 (a hysteria2-domain -acme
// challenge server, a caddy-mode panel server, or a naive server on :80).
//
// When this is true the panel IP-certificate renewal cannot run acme.sh
// --standalone on :80 — the bind would collide with Caddy's own listener —
// so the issuer parks on an internal port that the rendered
// /.well-known/acme-challenge/ reverse-proxy route forwards to (#1181).
// When Caddy does not own :80 (direct mode with no rendered :80 listener, or
// the challenge bind demoted because a foreign service holds the port), the
// standalone flow stays unchanged.
func PanelIPCertCaddyFronted(settings model.Settings, inbounds []model.Inbound) bool {
	if settings.PanelAccess != "direct" {
		return false
	}
	_, owners, _, err := BuildFinalRenderPlan(settings, inbounds)
	if err != nil {
		return false
	}
	owner, ok := owners[bindregistry.BindKey{Address: "0.0.0.0", Port: 80, Network: bindregistry.ListenTCP}]
	if !ok {
		return false
	}
	// Only a listener inside veil-caddy can front the challenge path; a
	// foreign or non-HTTP owner on :80 keeps the classic standalone
	// behaviour (which fails loudly on the busy port).
	return owner.ServiceName == "veil-caddy.service"
}
