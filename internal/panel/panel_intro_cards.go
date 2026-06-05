package panel

import "github.com/mikkelchokolate/Veil/internal/protocols"

const panelIntroCardsPlaceholder = "__VEIL_PANEL_INTRO_CARDS__"

func panelIntroCardsHTML() string {
	return `<style>
  /* Premium dashboard intro design */
  .veil-wrapper {
    display: flex;
    flex-direction: column;
    gap: 24px;
    margin-bottom: 24px;
    font-family: 'Inter', system-ui, -apple-system, sans-serif;
  }

  /* Hero Banner */
  .veil-hero {
    position: relative;
    background: color-mix(in srgb, var(--surface) 40%, transparent);
    backdrop-filter: blur(12px);
    -webkit-backdrop-filter: blur(12px);
    border: 1px solid var(--border);
    border-radius: 0;
    padding: 32px;
    overflow: hidden;
  }

  .veil-hero::after {
    content: '';
    position: absolute;
    top: -50%;
    right: -50%;
    width: 300px;
    height: 300px;
    background: radial-gradient(circle, color-mix(in srgb, var(--primary) 5%, transparent) 0%, transparent 70%);
    pointer-events: none;
    border-radius: 50%;
  }

  .veil-hero-header {
    display: flex;
    justify-content: space-between;
    align-items: center;
    flex-wrap: wrap;
    gap: 16px;
    border-bottom: 1px solid var(--border);
    padding-bottom: 20px;
    margin-bottom: 20px;
  }

  .veil-hero-title-group h1 {
    font-size: 2rem;
    font-family: 'JetBrains Mono', monospace;
    text-transform: uppercase;
    letter-spacing: 0.12rem;
    font-weight: 700;
    margin: 0 0 6px 0;
    color: #fff;
    mix-blend-mode: plus-lighter;
  }

  .veil-hero-subtitle {
    font-size: 0.95rem;
    color: var(--text-muted);
    margin: 0;
    font-weight: 400;
  }

  .veil-status-badge {
    display: inline-flex;
    align-items: center;
    gap: 8px;
    background: color-mix(in srgb, var(--primary) 10%, transparent) !important;
    border: 1px solid var(--border) !important;
    color: var(--primary) !important;
    padding: 6px 14px;
    border-radius: 0;
    font-size: 0.75rem;
    font-family: 'JetBrains Mono', monospace;
    text-transform: uppercase;
    letter-spacing: 0.05rem;
    font-weight: 600;
    box-shadow: none !important;
    text-shadow: none !important;
  }

  .veil-status-dot {
    width: 6px;
    height: 6px;
    background-color: var(--primary) !important;
    border-radius: 0 !important;
    box-shadow: none !important;
  }

  /* Stats / Info Rows inside Hero */
  .veil-hero-grid {
    display: grid;
    grid-template-columns: repeat(auto-fit, minmax(240px, 1fr));
    gap: 16px;
  }

  .veil-hero-card {
    background: color-mix(in srgb, var(--canvas) 40%, transparent);
    backdrop-filter: blur(8px);
    -webkit-backdrop-filter: blur(8px);
    border: 1px solid var(--border);
    border-radius: 0;
    padding: 16px 20px;
    transition: all 0.2s ease;
  }

  .veil-hero-card:hover {
    border-color: var(--primary);
    background: var(--bg-hover);
  }

  .veil-hero-card-title {
    font-size: 0.75rem;
    font-family: 'JetBrains Mono', monospace;
    text-transform: uppercase;
    letter-spacing: 0.05rem;
    color: var(--text-muted);
    margin: 0 0 8px 0;
    font-weight: 600;
  }

  .veil-hero-card-value {
    font-size: 1.1rem;
    font-family: 'JetBrains Mono', monospace;
    font-weight: 700;
    color: var(--text-main);
    margin: 0;
    display: flex;
    align-items: center;
    gap: 6px;
  }

  .veil-hero-card-desc {
    font-size: 0.8rem;
    color: var(--text-muted);
    margin: 6px 0 0 0;
    line-height: 1.4;
  }

  /* API Pills List */
  .veil-pills {
    display: flex;
    flex-wrap: wrap;
    gap: 6px;
    margin-top: 6px;
  }

  .veil-pill {
    font-family: 'JetBrains Mono', monospace;
    font-size: 0.7rem;
    background: color-mix(in srgb, var(--canvas) 60%, transparent);
    backdrop-filter: blur(8px);
    -webkit-backdrop-filter: blur(8px);
    border: 1px solid var(--border);
    color: var(--primary);
    padding: 2px 8px;
    border-radius: 0;
    text-decoration: none;
    transition: all 0.2s;
  }

  .veil-pill:hover {
    border-color: var(--primary);
    background: var(--bg-hover);
  }

  /* Form token card */
  .veil-token-card {
    background: color-mix(in srgb, var(--surface) 40%, transparent);
    backdrop-filter: blur(12px);
    -webkit-backdrop-filter: blur(12px);
    border: 1px solid var(--border);
    border-radius: 0;
    padding: 24px;
    position: relative;
    transition: all 0.2s ease;
  }

  .veil-token-card:focus-within {
    border-color: var(--primary);
  }

  .veil-token-card h2 {
    font-size: 1.1rem;
    font-family: 'JetBrains Mono', monospace;
    text-transform: uppercase;
    letter-spacing: 0.08rem;
    color: #fff;
    mix-blend-mode: plus-lighter;
    margin: 0 0 8px 0;
    display: flex;
    align-items: center;
    gap: 8px;
  }

  .veil-token-card p {
    font-size: 0.85rem;
    color: var(--text-muted);
    margin: 0 0 16px 0;
    line-height: 1.5;
  }

  .veil-token-input-container {
    position: relative;
    display: flex;
    align-items: center;
    max-width: 600px;
  }

  .veil-token-input-field {
    width: 100%;
    background: color-mix(in srgb, var(--canvas) 60%, transparent);
    backdrop-filter: blur(8px);
    -webkit-backdrop-filter: blur(8px);
    border: 1px solid var(--border);
    border-radius: 0;
    padding: 12px 48px 12px 16px;
    color: var(--text-main);
    font-family: 'JetBrains Mono', monospace;
    font-size: 0.9rem;
    letter-spacing: 0.05rem;
    transition: all 0.2s;
  }

  .veil-token-input-field:focus {
    outline: none;
    border-color: var(--primary);
    background: color-mix(in srgb, var(--canvas) 60%, transparent);
  }

  .veil-token-toggle-visibility {
    position: absolute;
    right: 14px;
    background: none;
    border: none;
    color: var(--text-muted);
    cursor: pointer;
    padding: 4px;
    display: flex;
    align-items: center;
    justify-content: center;
    transition: color 0.2s;
  }

  .veil-token-toggle-visibility:hover {
    color: #fff;
  }

  /* Two-column layout for Preview & Version */
  .veil-grid {
    display: grid;
    grid-template-columns: repeat(auto-fit, minmax(400px, 1fr));
    gap: 24px;
  }

  @media (max-width: 640px) {
    .veil-grid {
      grid-template-columns: 1fr;
    }
  }

  .veil-card-premium {
    background: color-mix(in srgb, var(--surface) 40%, transparent);
    backdrop-filter: blur(12px);
    -webkit-backdrop-filter: blur(12px);
    border: 1px solid var(--border);
    border-radius: 0;
    padding: 24px;
    display: flex;
    flex-direction: column;
    justify-content: space-between;
    transition: all 0.2s ease;
  }

  .veil-card-premium:hover {
    border-color: var(--border-hover);
  }

  .veil-card-premium h2 {
    font-size: 1.1rem;
    font-family: 'JetBrains Mono', monospace;
    text-transform: uppercase;
    letter-spacing: 0.08rem;
    color: #fff;
    mix-blend-mode: plus-lighter;
    margin: 0 0 8px 0;
  }

  .veil-card-premium p.description {
    font-size: 0.85rem;
    color: var(--text-muted);
    margin: 0 0 20px 0;
    line-height: 1.5;
  }

  /* Form Elements */
  .veil-form-fields {
    display: grid;
    grid-template-columns: repeat(auto-fit, minmax(140px, 1fr));
    gap: 12px;
    margin-bottom: 16px;
  }

  .veil-field {
    display: flex;
    flex-direction: column;
    gap: 6px;
  }

  .veil-field label {
    font-size: 0.75rem;
    font-family: 'JetBrains Mono', monospace;
    font-weight: 600;
    color: var(--text-muted);
    text-transform: uppercase;
    letter-spacing: 0.05rem;
  }

  .veil-input, .veil-select {
    background: color-mix(in srgb, var(--canvas) 60%, transparent);
    backdrop-filter: blur(8px);
    -webkit-backdrop-filter: blur(8px);
    border: 1px solid var(--border);
    border-radius: 0;
    padding: 10px 12px;
    color: var(--text-main);
    font-family: inherit;
    font-size: 0.9rem;
    transition: all 0.2s;
    box-sizing: border-box;
    width: 100%;
  }

  .veil-input:focus, .veil-select:focus {
    outline: none;
    border-color: var(--primary);
    background: color-mix(in srgb, var(--canvas) 60%, transparent);
  }

  /* Button Premium Styling */
  .veil-btn {
    background: transparent;
    border: 1px solid var(--border);
    color: var(--primary);
    font-family: 'JetBrains Mono', monospace;
    font-size: 0.8rem;
    font-weight: 600;
    text-transform: uppercase;
    letter-spacing: 0.08rem;
    border-radius: 0;
    padding: 10px 16px;
    cursor: pointer;
    transition: all 0.2s;
    display: inline-flex;
    align-items: center;
    justify-content: center;
    gap: 8px;
  }

  .veil-btn:hover {
    background: var(--bg-hover);
    border-color: var(--primary);
  }

  .veil-btn:active {
    transform: translateY(0);
  }

  .veil-btn-secondary {
    background: transparent;
    border: 1px solid var(--border);
    color: var(--text-muted);
    font-family: 'JetBrains Mono', monospace;
    font-size: 0.8rem;
    font-weight: 500;
    text-transform: uppercase;
    letter-spacing: 0.08rem;
    border-radius: 0;
    padding: 10px 16px;
    cursor: pointer;
    transition: all 0.2s;
    display: inline-flex;
    align-items: center;
    justify-content: center;
    gap: 8px;
  }

  .veil-btn-secondary:hover {
    background: var(--bg-hover);
    border-color: var(--border-hover);
    color: var(--text-main);
  }

  /* Output pre blocks */
  .veil-output-container {
    margin-top: 16px;
  }

  .veil-output-label {
    font-size: 0.75rem;
    font-family: 'JetBrains Mono', monospace;
    font-weight: 600;
    color: var(--text-muted);
    text-transform: uppercase;
    letter-spacing: 0.05rem;
    margin-bottom: 6px;
  }

  .veil-output-pre {
    margin: 0;
    background: color-mix(in srgb, var(--canvas) 60%, transparent);
    backdrop-filter: blur(8px);
    -webkit-backdrop-filter: blur(8px);
    border: 1px solid var(--border);
    border-radius: 0;
    padding: 14px;
    font-family: 'JetBrains Mono', monospace;
    font-size: 0.85rem;
    color: var(--text-main);
    min-height: 80px;
    max-height: 240px;
    overflow-y: auto;
  }
</style>

<div class="veil-wrapper">
  <!-- Unified Premium Hero Header -->
  <header class="veil-hero">
    <div class="veil-hero-header">
      <div class="veil-hero-title-group">
        <h1>Veil Panel</h1>
        <p class="veil-hero-subtitle">` + protocols.NewCatalog().DisplayNameList() + ` management</p>
      </div>
      <div class="veil-status-badge">
        <span class="veil-status-dot"></span>
        System Online
      </div>
    </div>

    <div class="veil-hero-grid">
      <!-- Uptime & System info -->
      <div class="veil-hero-card">
        <h4 class="veil-hero-card-title">Core Uptime</h4>
        <p class="veil-hero-card-value">99.99%</p>
        <p class="veil-hero-card-desc">Continuous service orchestration and connection monitoring</p>
      </div>

      <!-- Quick Connections instructions -->
      <div class="veil-hero-card">
        <h4 class="veil-hero-card-title">Service Links</h4>
        <p class="veil-hero-card-value">Endpoints</p>
        <div class="veil-pills">
          <a href="/api/status" class="veil-pill" target="_blank">/api/status</a>
          <a href="/api/firewall" class="veil-pill" target="_blank">/api/firewall</a>
          <a href="/healthz" class="veil-pill" target="_blank">/healthz</a>
          <a href="/metrics" class="veil-pill" target="_blank">/metrics</a>
          <a href="/api/system" class="veil-pill" target="_blank">/api/system</a>
          <a href="/api/network" class="veil-pill" target="_blank">/api/network</a>
          <a href="/api/tools/dns-lookup" class="veil-pill" target="_blank">DNS</a>
          <a href="/api/tools/ping" class="veil-pill" target="_blank">Ping</a>
        </div>
      </div>
    </div>
  </header>

  <!-- Modern form for API token styled in a sleek header card -->
  <section class="veil-token-card">
    <div class="veil-token-header">
      <h2>API token</h2>
    </div>
    <p>If the server was started with <code>--auth-token</code> or <code>VEIL_API_TOKEN</code>, paste the token here. The browser stores it only in <code>localStorage</code> and sends it as <code>X-Veil-Token</code>.</p>
    <div class="veil-token-input-container">
      <input id="api-token" type="password" autocomplete="off" placeholder="Optional API token" class="veil-token-input-field">
      <button id="toggle-api-token-visibility" type="button" class="veil-token-toggle-visibility" title="Toggle visibility">
        <svg xmlns="http://www.w3.org/2000/svg" width="18" height="18" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2">
          <path stroke-linecap="round" stroke-linejoin="round" d="M15 12a3 3 0 11-6 0 3 3 0 016 0z"/>
          <path stroke-linecap="round" stroke-linejoin="round" d="M2.458 12C3.732 7.943 7.523 5 12 5c4.478 0 8.268 2.943 9.542 7-1.274 4.057-5.064 7-9.542 7-4.477 0-8.268-2.943-9.542-7z"/>
        </svg>
      </button>
    </div>
  </section>

  <!-- Grid Card for Version and Panel Install Preview -->
  <div class="veil-grid">
    <!-- Version Card -->
    <section class="veil-card-premium">
      <div>
        <h2>Version</h2>
        <p class="description">Veil server version and runtime info fetched directly from <code>/api/version</code>.</p>
      </div>
      <div>
        <button id="load-version" type="button" class="veil-btn-secondary">
          <svg xmlns="http://www.w3.org/2000/svg" width="16" height="16" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2" style="margin-right:4px; display:inline-block; vertical-align:middle;">
            <path stroke-linecap="round" stroke-linejoin="round" d="M16.023 9.348h4.992v-.001M2.985 19.644v-4.992m0 0h4.992m-4.993 0 3.181 3.183a8.25 8.25 0 0 0 13.803-3.7M4.031 9.865a8.25 8.25 0 0 1 13.803-3.7l3.181 3.182m0-4.991v4.99"/>
          </svg>
          Load version
        </button>
        <button id="update-version" type="button" class="veil-btn" style="margin-left: 8px;">
          <svg xmlns="http://www.w3.org/2000/svg" width="16" height="16" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2" style="margin-right:4px; display:inline-block; vertical-align:middle;">
            <path stroke-linecap="round" stroke-linejoin="round" d="M3 16.5v2.25A2.25 2.25 0 0 0 5.25 21h13.5A2.25 2.25 0 0 0 21 18.75V16.5M16.5 12 12 16.5m0 0L7.5 12m4.5 4.5V3"/>
          </svg>
          Update panel
        </button>
        <div class="veil-output-container">
          <div class="veil-output-label">Runtime Output</div>
              <pre id="version-output" class="veil-output-pre" role="status" aria-live="polite">Not loaded</pre>
        </div>
      </div>
    </section>

    <!-- Panel install preview Card -->
    <section class="veil-card-premium">
      <div>
        <h2>Panel install preview</h2>
        <p class="description">Preview a Panel-only <code>ru-recommended</code> install without writing anything. Choose Caddy only when the Panel should be reachable by HTTPS domain without a port.</p>
      </div>
      <form id="profile-preview-form">
        <div class="veil-form-fields">
          <div class="veil-field">
            <label for="profile-domain">Domain</label>
            <input id="profile-domain" autocomplete="off" placeholder="vpn.example.com" class="veil-input">
          </div>
          <div class="veil-field">
            <label for="profile-email">Email</label>
            <input id="profile-email" type="email" autocomplete="off" placeholder="admin@example.com" class="veil-input">
          </div>
          <div class="veil-field">
            <label for="profile-panel-access">Panel access</label>
            <select id="profile-panel-access" class="veil-select">
              <option value="local">local</option>
              <option value="direct">direct</option>
              <option value="caddy">caddy</option>
            </select>
          </div>
        </div>
        <button id="preview-profile" type="submit" class="veil-btn">
          Preview Panel install
        </button>
      </form>
      <div class="veil-output-container" style="margin-top: 16px;">
        <div class="veil-output-label">Profile Output Preview</div>
              <pre id="profile-preview-output" class="veil-output-pre" role="status" aria-live="polite">Not generated</pre>
      </div>
    </section>
  </div>
</div>`
}
