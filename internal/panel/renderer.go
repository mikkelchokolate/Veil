package panel

import "strings"

type RenderSlot struct {
	Placeholder string
	Render      func() string
}

type Renderer struct {
	slots []RenderSlot
}

func NewRenderer(slots []RenderSlot) Renderer {
	return Renderer{slots: append([]RenderSlot(nil), slots...)}
}

func (r Renderer) HTML(basePath string) string {
	html := r.BaseHTML()
	if basePath == "" || basePath == "/" {
		return html
	}
	bp := strings.TrimRight(basePath, "/")
	replacer := strings.NewReplacer(
		`"/api/`, `"`+bp+`/api/`,
		`'/api/`, `'`+bp+`/api/`,
		`"/healthz`, `"`+bp+`/healthz`,
		`'/healthz`, `'`+bp+`/healthz`,
		`"/metrics`, `"`+bp+`/metrics`,
		`'/metrics`, `'`+bp+`/metrics`,
	)
	return replacer.Replace(html)
}

func (r Renderer) BaseHTML() string {
	html := panelHTMLBase
	for _, slot := range r.slots {
		if slot.Render == nil {
			continue
		}
		html = strings.ReplaceAll(html, slot.Placeholder, slot.Render())
	}
	return html
}

// panelHTMLBase is the raw panel HTML. Paths in JS strings are all /-prefixed
// (e.g., "/api/status"). At serve time, a replacer injects the web base path.
const panelHTMLBase = `<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>Veil Panel</title>
  <style>
    body { margin: 0; font-family: Inter, system-ui, sans-serif; background: #070a12; color: #e6edf3; }
    main { max-width: 1180px; margin: 0 auto; padding: 48px 24px; }
    .grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(320px, 1fr)); gap: 16px; }
    .card { border: 1px solid #263043; border-radius: 16px; padding: 24px; margin: 16px 0; background: #0d111c; }
    .form-grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(180px, 1fr)); gap: 12px; margin: 12px 0; }
    .actions { display: flex; flex-wrap: wrap; gap: 8px; margin: 12px 0; }
    code { color: #8be9fd; }
    label { display: block; margin-bottom: 8px; color: #9fb0c3; }
    input, select, textarea { box-sizing: border-box; width: 100%; border: 1px solid #263043; border-radius: 10px; padding: 10px 12px; background: #070a12; color: #e6edf3; }
    input[type="checkbox"] { width: auto; margin-right: 8px; }
    button { border: 0; border-radius: 10px; padding: 10px 14px; background: #4f46e5; color: white; cursor: pointer; }
    button.secondary { background: #334155; }
    button.danger { background: #dc2626; }
    pre { overflow: auto; border-radius: 10px; padding: 12px; background: #070a12; color: #c9d1d9; min-height: 72px; }
    .hint { color: #9fb0c3; font-size: 0.92rem; }
  </style>
</head>
<body>
  <main>
__VEIL_PANEL_INTRO_CARDS__
__VEIL_PANEL_SERVICE_STATUS_CARD__
__VEIL_PANEL_RUNTIME_STATS_CARDS__
__VEIL_PANEL_CLIENT_LINKS_CARD__

    <div class="grid">
__VEIL_PANEL_SETTINGS_CARD__
__VEIL_PANEL_INBOUND_FORM__
    </div>

    <div class="grid">
__VEIL_PANEL_ROUTING_CARD__

__VEIL_PANEL_WARP_CARD__
    </div>

__VEIL_PANEL_APPLY_CARD__
__VEIL_PANEL_DIAGNOSTICS_CARDS__
  </main>
  <script>
__VEIL_PANEL_INTRO_ACTIONS__

__VEIL_PANEL_UTILITY_ACTIONS__

__VEIL_PANEL_SETTINGS_ACTIONS__

__VEIL_PANEL_SERVICE_STATUS_ACTIONS__

__VEIL_PANEL_CLIENT_LINKS_ACTIONS__

__VEIL_PANEL_INBOUND_ACTIONS__

__VEIL_PANEL_CLIENT_PROFILE_ACTIONS__

__VEIL_PANEL_WARP_ACTIONS__

__VEIL_PANEL_ROUTING_ACTIONS__

__VEIL_PANEL_SERVICE_RESTART_ACTIONS__
__VEIL_PANEL_RUNTIME_STATS_ACTIONS__
__VEIL_PANEL_APPLY_ACTIONS__
__VEIL_PANEL_DIAGNOSTICS_ACTIONS__
__VEIL_PANEL_EVENT_BINDINGS__
  </script>
</body>
</html>
`
