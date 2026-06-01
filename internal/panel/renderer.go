package panel

import (
	"regexp"
	"strings"
)

var placeholderRegex = regexp.MustCompile(`__VEIL_[A-Z_]+__`)

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
	html = strings.ReplaceAll(html, "__PROMETHEUS_BG_BASE64__", prometheusBgBase64)
	for _, slot := range r.slots {
		if slot.Render == nil {
			continue
		}
		html = strings.ReplaceAll(html, slot.Placeholder, slot.Render())
	}

	// Clean up any remaining placeholders (e.g. from disabled modules)
	html = placeholderRegex.ReplaceAllString(html, "")
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
  <link rel="icon" type="image/svg+xml" href="data:image/svg+xml,%3Csvg xmlns='http://www.w3.org/2000/svg' viewBox='0 0 24 24' fill='%23ffe6cb'%3E%3Cpath d='M20 4H4c-1.1 0-1.99.9-1.99 2L2 18c0 1.1.9 2 2 2h16c1.1 0 2-.9 2-2V6c0-1.1-.9-2-2-2zm-9 12H5v-2h6v2zm4-4H5v-2h10v2zm4-4H5V6h14v2z'/%3E%3C/svg%3E">
  <link rel="preconnect" href="https://fonts.googleapis.com">
  <link rel="preconnect" href="https://fonts.gstatic.com" crossorigin>
  <link href="https://fonts.googleapis.com/css2?family=Inter:wght@400;500;600;700&family=JetBrains+Mono:wght@400;500;600;700&display=swap" rel="stylesheet">
  <style>
    :root {
      --canvas: #060d0d;
      --surface: #0a1414;
      --primary: #ffe6cb;
      --accent: #8b5cf6;
      --accent-success: #10b981;
      --accent-danger: #ef4444;
      --accent-warning: #f59e0b;
      
      /* Derived colors using color-mix */
      --border: color-mix(in srgb, var(--primary) 15%, transparent);
      --border-hover: color-mix(in srgb, var(--primary) 30%, transparent);
      --bg-hover: color-mix(in srgb, var(--primary) 5%, transparent);
      --text-main: var(--primary);
      --text-muted: color-mix(in srgb, var(--primary) 60%, transparent);
      --text-disabled: color-mix(in srgb, var(--primary) 30%, transparent);
    }

    html {
      background-color: var(--canvas);
      scrollbar-gutter: stable;
    }
    body {
      margin: 0;
      font-family: 'Inter', system-ui, -apple-system, sans-serif;
      background-color: transparent;
      color: var(--text-main);
      display: flex;
      min-height: 100vh;
      overflow-x: hidden;
    }

    /* Global Noise/Grain Overlay */
    body::after {
      content: "";
      position: fixed;
      top: 0;
      left: 0;
      width: 100vw;
      height: 100vh;
      z-index: 999999;
      pointer-events: none;
      opacity: 0.28;
      background-image: url("data:image/svg+xml,%3Csvg viewBox='0 0 200 200' xmlns='http://www.w3.org/2000/svg'%3E%3Cfilter id='noiseFilter'%3E%3CfeTurbulence type='fractalNoise' baseFrequency='0.95' numOctaves='4' stitchTiles='stitch'/%3E%3C/filter%3E%3Crect width='100%25' height='100%25' filter='url(%23noiseFilter)'/%3E%3C/svg%3E");
      mix-blend-mode: overlay;
    }

    /* Custom scrollbars */
    ::-webkit-scrollbar {
      width: 8px;
      height: 8px;
    }
    ::-webkit-scrollbar-track {
      background: var(--canvas);
      border-left: 1px solid var(--border);
      border-top: 1px solid var(--border);
    }
    ::-webkit-scrollbar-thumb {
      background: var(--border);
      border-radius: 0;
    }
    ::-webkit-scrollbar-thumb:hover {
      background: var(--primary);
    }

    /* Fixed Background Image and Overlay */
    .bg-overlay-container {
      position: fixed;
      top: 0;
      left: 0;
      right: 0;
      bottom: 0;
      z-index: -1;
      pointer-events: none;
      background-color: var(--canvas);
    }
    .bg-image {
      position: absolute;
      top: 0;
      left: 0;
      width: 100%;
      height: 100%;
      background-image: url('data:image/jpeg;base64,__PROMETHEUS_BG_BASE64__');
      background-size: cover;
      background-position: left center;
      background-repeat: no-repeat;
      opacity: 0.08;
      filter: saturate(25%) contrast(130%) brightness(85%);
    }

    /* Sidebar Navigation (Hermes hallmark border-grid) */
    .sidebar {
      width: 260px;
      background: color-mix(in srgb, var(--surface) 60%, transparent);
      backdrop-filter: blur(16px);
      -webkit-backdrop-filter: blur(16px);
      border-right: 1px solid var(--border);
      display: flex;
      flex-direction: column;
      position: fixed;
      top: 0;
      bottom: 0;
      left: 0;
      z-index: 100;
    }
    .logo {
      padding: 24px;
      font-size: 0.95rem;
      font-weight: 700;
      text-transform: uppercase;
      letter-spacing: 0.15rem;
      color: #fff;
      mix-blend-mode: plus-lighter;
      border-bottom: 1px solid var(--border);
      display: flex;
      align-items: center;
      gap: 10px;
      font-family: 'JetBrains Mono', monospace;
    }
    .nav-menu {
      padding: 0;
      display: flex;
      flex-direction: column;
    }
    .nav-item {
      display: flex;
      align-items: center;
      gap: 12px;
      padding: 16px 24px;
      color: var(--text-muted);
      text-decoration: none;
      font-family: 'JetBrains Mono', monospace;
      text-transform: uppercase;
      letter-spacing: 0.1rem;
      font-size: 0.8rem;
      border-bottom: 1px solid var(--border);
      transition: all 0.2s ease;
      cursor: pointer;
    }
    .nav-item:hover {
      color: #fff;
      background: var(--bg-hover);
    }
    .nav-item.active {
      color: #fff;
      background: var(--bg-hover);
      border-left: 3px solid var(--primary);
    }

    /* Content Wrapper */
    .content-wrapper {
      margin-left: 260px;
      flex: 1;
      display: flex;
      flex-direction: column;
      min-width: 0;
      position: relative;
      z-index: 1;
    }
    .top-bar {
      height: 70px;
      background: color-mix(in srgb, var(--surface) 40%, transparent);
      backdrop-filter: blur(12px);
      -webkit-backdrop-filter: blur(12px);
      border-bottom: 1px solid var(--border);
      display: flex;
      align-items: center;
      justify-content: space-between;
      padding: 0 32px;
    }
    .breadcrumb {
      font-family: 'JetBrains Mono', monospace;
      text-transform: uppercase;
      letter-spacing: 0.05rem;
      font-size: 0.85rem;
      font-weight: 600;
      color: var(--text-muted);
    }
    .breadcrumb span {
      color: #fff;
      mix-blend-mode: plus-lighter;
    }
    
    main {
      padding: 32px;
      max-width: 1200px;
      width: 100%;
      box-sizing: border-box;
    }

    /* Cards & Grids (Border-grid inspired, no rounded corners, mix-blend-mode plus-lighter) */
    .card {
      background: color-mix(in srgb, var(--surface) 40%, transparent);
      backdrop-filter: blur(12px);
      -webkit-backdrop-filter: blur(12px);
      border: 1px solid var(--border);
      border-radius: 0;
      padding: 24px;
      margin-bottom: 24px;
      transition: border-color 0.2s ease;
    }
    .card:hover {
      border-color: var(--border-hover);
    }
    .card h2 {
      margin-top: 0;
      font-size: 1.1rem;
      font-family: 'JetBrains Mono', monospace;
      text-transform: uppercase;
      letter-spacing: 0.08rem;
      display: flex;
      align-items: center;
      gap: 8px;
      color: #fff;
      mix-blend-mode: plus-lighter;
      border-bottom: 1px solid var(--border);
      padding-bottom: 12px;
      margin-bottom: 20px;
    }
    .grid {
      display: grid;
      grid-template-columns: repeat(auto-fit, minmax(320px, 1fr));
      gap: 20px;
    }
    .form-grid {
      display: grid;
      grid-template-columns: repeat(auto-fit, minmax(200px, 1fr));
      gap: 16px;
      margin: 16px 0;
    }
    .actions {
      display: flex;
      flex-wrap: wrap;
      gap: 8px;
      margin: 12px 0;
    }

    /* Form Elements */
    label {
      display: block;
      margin-bottom: 8px;
      font-size: 0.8rem;
      font-family: 'JetBrains Mono', monospace;
      text-transform: uppercase;
      letter-spacing: 0.05rem;
      color: var(--text-muted);
      font-weight: 500;
    }
    input, select, textarea {
      box-sizing: border-box;
      width: 100%;
      border: 1px solid var(--border);
      border-radius: 0;
      padding: 12px 16px;
      background: color-mix(in srgb, var(--canvas) 60%, transparent);
      backdrop-filter: blur(8px);
      -webkit-backdrop-filter: blur(8px);
      color: var(--text-main);
      font-family: inherit;
      transition: border-color 0.2s;
    }
    input:focus, select:focus, textarea:focus {
      outline: none;
      border-color: var(--primary);
    }
    select {
      appearance: none !important;
      -webkit-appearance: none !important;
      -moz-appearance: none !important;
      padding-right: 32px !important;
      background-color: color-mix(in srgb, var(--canvas) 60%, transparent) !important;
      background-image: url("data:image/svg+xml,%3Csvg xmlns='http://www.w3.org/2000/svg' viewBox='0 0 10 6' fill='%23ffe6cb'%3E%3Cpath d='M0 0h10L5 6z'/%3E%3C/svg%3E") !important;
      background-repeat: no-repeat !important;
      background-position: right 10px center !important;
      background-size: 12px 7px !important;
      cursor: pointer;
    }
    select option {
      background: var(--canvas);
      color: var(--text-main);
      padding: 8px 16px;
      border: none;
    }
    select option:checked,
    select option:hover {
      background: color-mix(in srgb, var(--primary) 30%, var(--canvas));
    }
    input[type="checkbox"] {
      width: auto;
      margin-right: 8px;
      transform: scale(1.1);
    }

    /* Buttons */
    button {
      display: inline-flex;
      align-items: center;
      justify-content: center;
      gap: 8px;
      border: 1px solid var(--border);
      border-radius: 0;
      padding: 12px 20px;
      background: transparent;
      color: var(--primary);
      font-family: 'JetBrains Mono', monospace;
      font-weight: 600;
      text-transform: uppercase;
      letter-spacing: 0.08rem;
      font-size: 0.8rem;
      cursor: pointer;
      transition: background-color 0.2s, border-color 0.2s, color 0.2s;
    }
    button:hover {
      background: var(--bg-hover);
      border-color: var(--primary);
    }
    button:active {
      transform: scale(0.98);
    }
    button.secondary {
      border-color: var(--border);
      color: var(--text-muted);
    }
    button.secondary:hover {
      border-color: var(--border-hover);
      color: var(--text-main);
    }
    button.danger {
      border-color: var(--accent-danger);
      color: var(--accent-danger);
    }
    button.danger:hover {
      background: color-mix(in srgb, var(--accent-danger) 15%, transparent);
      border-color: var(--accent-danger);
    }

    /* Output Boxes */
    pre {
      overflow: auto;
      border-radius: 0;
      padding: 16px;
      background: var(--canvas);
      color: var(--primary);
      font-family: 'JetBrains Mono', monospace;
      font-size: 0.85rem;
      border: 1px solid var(--border);
      min-height: 72px;
    }
    code {
      font-family: 'JetBrains Mono', monospace;
      color: var(--accent);
    }
    .hint {
      color: var(--text-muted);
      font-size: 0.85rem;
    }

    /* Tab Page Toggling */
    .tab-content {
      display: none;
    }
    .tab-content.active {
      display: block;
    }

    /* Badges & Pulses */
    .badge {
      display: inline-flex;
      align-items: center;
      gap: 6px;
      padding: 6px 12px;
      border-radius: 0;
      font-size: 0.75rem;
      font-family: 'JetBrains Mono', monospace;
      text-transform: uppercase;
      letter-spacing: 0.05rem;
      font-weight: 600;
      background: color-mix(in srgb, var(--primary) 15%, transparent);
      color: var(--primary);
      border: 1px solid var(--border);
    }
    .badge-success {
      background: color-mix(in srgb, var(--primary) 10%, transparent) !important;
      color: var(--primary) !important;
      border: 1px solid var(--border) !important;
      box-shadow: none !important;
      text-shadow: none !important;
    }
    .pulse-static {
      display: inline-block;
      width: 6px;
      height: 6px;
      background-color: var(--primary) !important;
      border-radius: 0 !important;
      box-shadow: none !important;
    }

    /* Premium control panel layout enhancements */
    .telemetry-grid {
      display: grid;
      grid-template-columns: repeat(auto-fit, minmax(220px, 1fr));
      gap: 20px;
      margin-bottom: 24px;
    }
    .telemetry-card {
      background: var(--surface);
      border: 1px solid var(--border);
      border-radius: 0;
      padding: 24px;
      display: flex;
      flex-direction: column;
      align-items: center;
      text-align: center;
      position: relative;
      transition: border-color 0.2s ease;
    }
    .telemetry-card:hover {
      border-color: var(--primary);
    }
    .telemetry-card h3 {
      margin: 0 0 16px 0;
      font-size: 0.85rem;
      font-family: 'JetBrains Mono', monospace;
      text-transform: uppercase;
      letter-spacing: 0.05rem;
      font-weight: 600;
      color: var(--text-muted);
    }
    .circle-chart-container {
      position: relative;
      width: 120px;
      height: 120px;
    }
    .circle-chart {
      width: 100%;
      height: 100%;
      transform: rotate(-90deg);
    }
    .circle-chart__circle {
      transition: stroke-dashoffset 0.6s ease-in-out;
    }
    .circle-chart__info {
      position: absolute;
      top: 50%;
      left: 50%;
      transform: translate(-50%, -50%);
      display: flex;
      flex-direction: column;
      align-items: center;
    }
    .percent-val {
      font-size: 1.4rem;
      font-family: 'JetBrains Mono', monospace;
      font-weight: 700;
      color: #fff;
    }
    .percent-label {
      font-size: 0.7rem;
      font-family: 'JetBrains Mono', monospace;
      text-transform: uppercase;
      letter-spacing: 0.02rem;
      color: var(--text-muted);
      margin-top: 2px;
    }
    .telemetry-details {
      margin-top: 16px;
      font-size: 0.8rem;
      font-family: 'JetBrains Mono', monospace;
      color: var(--text-muted);
      font-weight: 500;
    }

    /* Modal Dialog Overlays */
    .modal-overlay {
      position: fixed;
      top: 0;
      left: 0;
      right: 0;
      bottom: 0;
      background: rgba(6, 13, 13, 0.6);
      backdrop-filter: blur(8px);
      -webkit-backdrop-filter: blur(8px);
      z-index: 1000;
      display: flex;
      align-items: center;
      justify-content: center;
      opacity: 0;
      pointer-events: none;
      transition: opacity 0.3s ease;
    }
    .modal-overlay.active {
      opacity: 1;
      pointer-events: auto;
    }
    .modal-content {
      background: color-mix(in srgb, var(--surface) 85%, transparent);
      backdrop-filter: blur(20px);
      -webkit-backdrop-filter: blur(20px);
      border: 1px solid var(--border);
      border-radius: 0;
      width: 90%;
      max-width: 600px;
      max-height: 85vh;
      overflow-y: auto;
      padding: 32px;
      position: relative;
      box-shadow: 0 10px 40px rgba(0, 0, 0, 0.5);
      transform: scale(0.9);
      transition: transform 0.3s ease;
    }
    .modal-overlay.active .modal-content {
      transform: scale(1);
    }
    .modal-header {
      display: flex;
      justify-content: space-between;
      align-items: center;
      margin-bottom: 24px;
      border-bottom: 1px solid var(--border);
      padding-bottom: 16px;
    }
    .modal-header h2 {
      margin: 0;
      font-size: 1.2rem;
      font-family: 'JetBrains Mono', monospace;
      text-transform: uppercase;
      letter-spacing: 0.08rem;
      color: #fff;
      mix-blend-mode: plus-lighter;
    }
    .modal-close {
      background: transparent;
      border: 1px solid var(--border);
      border-radius: 0;
      color: var(--text-muted);
      font-size: 1rem;
      font-family: 'JetBrains Mono', monospace;
      padding: 6px 12px;
      cursor: pointer;
      line-height: 1;
      transition: all 0.2s;
    }
    .modal-close:hover {
      background: var(--bg-hover);
      border-color: var(--primary);
      color: #fff;
    }

    /* Premium Tables & Datatables */
    .table-container {
      overflow-x: auto;
      border-radius: 0;
      border: 1px solid var(--border);
      background: var(--surface);
      margin-top: 16px;
    }
    table {
      width: 100%;
      border-collapse: collapse;
      text-align: left;
      font-size: 0.9rem;
    }
    th {
      background: var(--canvas);
      color: var(--text-muted);
      font-family: 'JetBrains Mono', monospace;
      font-weight: 600;
      padding: 14px 18px;
      border-bottom: 1px solid var(--border);
      font-size: 0.8rem;
      text-transform: uppercase;
      letter-spacing: 0.05rem;
    }
    td {
      padding: 14px 18px;
      border-bottom: 1px solid var(--border);
      color: var(--text-main);
      vertical-align: middle;
    }
    tr:last-child td {
      border-bottom: 0;
    }
    tr:hover td {
      background: var(--bg-hover);
    }

    /* Action Dropdown Buttons */
    .dropdown {
      position: relative;
      display: inline-block;
    }
    .dropdown-btn {
      background: transparent;
      border: 1px solid var(--border);
      padding: 8px 14px;
      border-radius: 0;
      font-size: 0.8rem;
      font-family: 'JetBrains Mono', monospace;
      text-transform: uppercase;
      letter-spacing: 0.03rem;
      color: var(--text-main);
    }
    .dropdown-content {
      display: none;
      position: absolute;
      right: 0;
      background: var(--surface);
      border: 1px solid var(--border);
      border-radius: 0;
      min-width: 160px;
      box-shadow: 0 8px 24px rgba(0, 0, 0, 0.4);
      z-index: 150;
      overflow: hidden;
      margin-top: 4px;
    }
    .dropdown-content button, .dropdown-content a {
      color: var(--text-main);
      padding: 12px 16px;
      text-decoration: none;
      display: block;
      width: 100%;
      text-align: left;
      background: transparent;
      border: 0;
      border-bottom: 1px solid var(--border);
      border-radius: 0;
      font-family: 'JetBrains Mono', monospace;
      font-size: 0.8rem;
      font-weight: 500;
      text-transform: uppercase;
      letter-spacing: 0.02rem;
      cursor: pointer;
      transition: background 0.2s;
      box-sizing: border-box;
    }
    .dropdown-content button:last-child, .dropdown-content a:last-child {
      border-bottom: 0;
    }
    .dropdown-content button:hover, .dropdown-content a:hover {
      background: var(--bg-hover);
      color: #fff;
    }
    .dropdown:hover .dropdown-content {
      display: block;
    }

    /* Toggle switches */
    .switch {
      position: relative;
      display: inline-block;
      width: 44px;
      height: 24px;
      vertical-align: middle;
    }
    .switch input {
      opacity: 0;
      width: 0;
      height: 0;
    }
    .slider {
      position: absolute;
      cursor: pointer;
      top: 0;
      left: 0;
      right: 0;
      bottom: 0;
      background-color: var(--canvas);
      border: 1px solid var(--border);
      transition: .3s;
      border-radius: 0;
    }
    .slider:before {
      position: absolute;
      content: "";
      height: 14px;
      width: 14px;
      left: 4px;
      bottom: 4px;
      background-color: var(--text-muted);
      transition: .3s;
      border-radius: 0;
    }
    input:checked + .slider {
      background-color: color-mix(in srgb, var(--accent-success) 15%, transparent);
      border-color: var(--accent-success);
    }
    input:checked + .slider:before {
      background-color: var(--accent-success);
      transform: translateX(20px);
    }

    /* Pseudo-Terminal Mockups */
    .terminal-window {
      background: #020404;
      border: 1px solid var(--border);
      border-radius: 0;
      overflow: hidden;
      margin-top: 16px;
    }
    .terminal-header {
      background: var(--surface);
      padding: 12px 18px;
      display: flex;
      align-items: center;
      justify-content: center;
      border-bottom: 1px solid var(--border);
    }
    .terminal-title {
      font-size: 0.8rem;
      font-family: 'JetBrains Mono', monospace;
      text-transform: uppercase;
      letter-spacing: 0.05rem;
      color: var(--text-muted);
    }
    .terminal-body {
      padding: 18px;
      margin: 0;
      font-family: 'JetBrains Mono', monospace;
      font-size: 0.85rem;
      color: var(--primary);
      line-height: 1.5;
      overflow-y: auto;
      max-height: 400px;
      white-space: pre-wrap;
    }
  </style>
</head>
<body>
  <!-- Fixed background overlay -->
  <div class="bg-overlay-container">
    <div class="bg-image"></div>
  </div>

  <!-- SidebarPERSISTENT -->
  <aside class="sidebar">
    <div class="logo">
      Veil Panel
    </div>
    <nav class="nav-menu">
      <a href="#dashboard" class="nav-item active" onclick="switchTab('dashboard')">
        Dashboard
      </a>
      <a href="#inbounds" class="nav-item" onclick="switchTab('inbounds')">
        Inbounds
      </a>
      <a href="#routing" class="nav-item" onclick="switchTab('routing')">
        Routing Rules
      </a>
      <a href="#warp" class="nav-item" onclick="switchTab('warp')">
        WARP
      </a>
      <a href="#diagnostics" class="nav-item" onclick="switchTab('diagnostics')">
        System Tools
      </a>
    </nav>
  </aside>

  <!-- PageWrapper -->
  <div class="content-wrapper">
    <header class="top-bar">
      <div class="breadcrumb">Veil Panel / <span id="current-page-title">Dashboard</span></div>
      <div class="status-indicator">
        API Service: <span class="badge badge-success"><span class="pulse-static"></span> ONLINE</span>
      </div>
    </header>

    <main>
      <!-- Section: Dashboard -->
      <div id="dashboard" class="tab-content active">
__VEIL_PANEL_INTRO_CARDS__
__VEIL_PANEL_RUNTIME_STATS_CARDS__
__VEIL_PANEL_SERVICE_STATUS_CARD__
      </div>

      <!-- Section: Inbounds -->
      <div id="inbounds" class="tab-content">
__VEIL_PANEL_INBOUND_FORM__
__VEIL_PANEL_CLIENT_LINKS_CARD__
      </div>

      <!-- Section: Routing -->
      <div id="routing" class="tab-content">
__VEIL_PANEL_ROUTING_CARD__
      </div>

      <!-- Section: WARP -->
      <div id="warp" class="tab-content">
__VEIL_PANEL_WARP_CARD__
      </div>

      <!-- Section: Diagnostics & Apply -->
      <div id="diagnostics" class="tab-content">
__VEIL_PANEL_APPLY_CARD__
__VEIL_PANEL_DIAGNOSTICS_CARDS__
      </div>
    </main>
  </div>

  <script>
    function switchTab(tabId) {
      document.querySelectorAll('.tab-content').forEach(el => el.classList.remove('active'));
      const activeTab = document.getElementById(tabId);
      if (activeTab) activeTab.classList.add('active');
      
      document.querySelectorAll('.nav-item').forEach(el => el.classList.remove('active'));
      const activeLink = document.querySelector('.nav-item[href="#' + tabId + '"]');
      if (activeLink) activeLink.classList.add('active');
      
      const pageNames = {
        'dashboard': 'Dashboard',
        'inbounds': 'Inbounds',
        'routing': 'Routing Rules',
        'warp': 'WARP',
        'diagnostics': 'System Tools'
      };
      document.getElementById('current-page-title').innerText = pageNames[tabId] || 'Dashboard';
      window.scrollTo(0, 0);
    }

    // Handle hash reload
    window.addEventListener('DOMContentLoaded', () => {
      const hash = window.location.hash.substring(1);
      if (['dashboard', 'inbounds', 'routing', 'warp', 'diagnostics'].includes(hash)) {
        switchTab(hash);
      }
    });

__VEIL_PANEL_INTRO_ACTIONS__

__VEIL_PANEL_UTILITY_ACTIONS__

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
