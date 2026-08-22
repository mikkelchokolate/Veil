package api

import (
	"fmt"
	"html"
	"net/http"
	"strings"

	"github.com/mikkelchokolate/Veil/internal/client"
	"github.com/mikkelchokolate/Veil/internal/model"
)

// writeSubscriptionHTML renders a minimal landing page for browsers that open
// a subscription link directly. It shows the profile title, the rendered node
// count, and copy-pasteable links. This is purely presentational; the machine
// formats remain the canonical delivery.
func (s *managementState) writeSubscriptionHTML(w http.ResponseWriter, cl client.View, links []model.ClientLink) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	title := html.EscapeString(subscriptionProfileTitle(cl))
	var b strings.Builder
	b.WriteString("<!doctype html><html><head><meta charset=\"utf-8\">")
	b.WriteString("<meta name=\"viewport\" content=\"width=device-width,initial-scale=1\">")
	fmt.Fprintf(&b, "<title>%s — Veil Subscription</title>", title)
	b.WriteString("<style>body{font-family:system-ui,sans-serif;background:#0b0e14;color:#e6e6e6;margin:0;padding:2rem;display:flex;justify-content:center}")
	b.WriteString(".card{max-width:640px;width:100%;background:#151a23;border:1px solid #232a36;border-radius:12px;padding:1.5rem}")
	b.WriteString("h1{font-size:1.25rem;margin:0 0 .5rem}p{color:#9aa4b2}code{background:#0b0e14;padding:.15rem .4rem;border-radius:6px;word-break:break-all;display:block;margin:.5rem 0}")
	b.WriteString(".node{background:#0b0e14;border:1px solid #232a36;border-radius:8px;padding:.5rem .75rem;margin:.4rem 0;font-size:.85rem;word-break:break-all}")
	b.WriteString("</style></head><body><div class=\"card\">")
	fmt.Fprintf(&b, "<h1>%s</h1>", title)
	fmt.Fprintf(&b, "<p>%d node(s) in this subscription.</p>", len(links))
	if cl.QuotaBytes != nil {
		fmt.Fprintf(&b, "<p>Quota: %d bytes.</p>", *cl.QuotaBytes)
	}
	for _, l := range links {
		fmt.Fprintf(&b, "<div class=\"node\">%s</div>", html.EscapeString(l.URI))
	}
	b.WriteString("<p style=\"margin-top:1rem\">Use this URL in a proxy client that supports subscriptions.</p>")
	b.WriteString("</div></body></html>")
	_, _ = w.Write([]byte(b.String()))
}
