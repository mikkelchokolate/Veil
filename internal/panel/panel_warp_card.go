package panel

const panelWarpCardPlaceholder = "__VEIL_PANEL_WARP_CARD__"

func panelWarpCardHTML() string {
	return `      <style>
        /* Premium WARP Card Styles */
        .warp-card {
          background: color-mix(in srgb, var(--surface) 80%, transparent) !important;
          border: 1px solid color-mix(in srgb, var(--border) 80%, transparent) !important;
          position: relative;
          overflow: hidden;
          transition: border-color 0.2s ease, box-shadow 0.2s ease;
          border-radius: 0 !important;
        }
        .warp-card:hover {
          border-color: var(--primary) !important;
          box-shadow: 0 0 15px color-mix(in srgb, var(--primary) 20%, transparent) !important;
        }

        .card-header-warp {
          border-bottom: 1px solid color-mix(in srgb, var(--border) 60%, transparent);
          padding-bottom: 16px;
          margin-bottom: 20px;
        }

        .warp-header-title h2 {
          margin: 0 0 6px 0 !important;
          font-size: 1.1rem;
          font-family: 'JetBrains Mono', monospace;
          text-transform: uppercase;
          letter-spacing: 0.08rem;
          color: #fff;
          mix-blend-mode: plus-lighter;
          text-shadow: 0 0 10px color-mix(in srgb, var(--primary) 40%, transparent);
        }

        .warp-header-title .hint {
          margin: 0 0 4px 0;
          color: var(--text-muted);
          font-size: 0.85rem;
        }

        .redacted-note {
          font-size: 0.8rem !important;
          color: var(--text-muted) !important;
        }

        .warp-status-row {
          display: flex;
          justify-content: space-between;
          align-items: center;
          background: color-mix(in srgb, var(--surface) 50%, transparent);
          border: 1px solid color-mix(in srgb, var(--border) 60%, transparent);
          border-radius: 0;
          padding: 16px 20px;
          margin-bottom: 24px;
        }

        .warp-status-info {
          display: flex;
          flex-direction: column;
          gap: 4px;
        }

        .warp-status-label {
          font-weight: 600;
          color: white;
          font-size: 0.95rem;
          font-family: 'JetBrains Mono', monospace;
          text-transform: uppercase;
          letter-spacing: 0.05rem;
          text-shadow: 0 0 8px color-mix(in srgb, #fff 30%, transparent);
        }

        .warp-status-sub {
          color: var(--text-muted);
          font-size: 0.8rem;
        }

        /* Three Column Grid for fields */
        .warp-grid {
          display: grid;
          grid-template-columns: repeat(3, 1fr);
          gap: 20px;
          margin-bottom: 24px;
        }

        @media (max-width: 1024px) {
          .warp-grid {
            grid-template-columns: repeat(2, 1fr);
          }
        }

        @media (max-width: 640px) {
          .warp-grid {
            grid-template-columns: 1fr;
          }
        }

        .warp-field-section {
          background: color-mix(in srgb, var(--surface) 40%, transparent);
          border: 1px solid color-mix(in srgb, var(--border) 50%, transparent);
          border-radius: 0;
          padding: 18px;
          display: flex;
          flex-direction: column;
          gap: 12px;
          transition: border-color 0.2s ease;
        }

        .warp-field-section:hover {
          border-color: color-mix(in srgb, var(--primary) 50%, transparent);
        }

        .warp-field-section .section-title {
          display: flex;
          align-items: center;
          gap: 8px;
          color: var(--primary);
          font-family: 'JetBrains Mono', monospace;
          text-transform: uppercase;
          letter-spacing: 0.05rem;
          font-weight: 600;
          font-size: 0.9rem;
          border-bottom: 1px solid color-mix(in srgb, var(--border) 60%, transparent);
          padding-bottom: 8px;
          margin-bottom: 4px;
          text-shadow: 0 0 8px color-mix(in srgb, var(--primary) 40%, transparent);
        }

        .field-group {
          display: flex;
          flex-direction: column;
          gap: 6px;
        }

        .field-group label {
          color: var(--text-muted);
          font-size: 0.75rem;
          font-family: 'JetBrains Mono', monospace;
          text-transform: uppercase;
          letter-spacing: 0.03rem;
          margin: 0;
          font-weight: 500;
        }

        .field-group input {
          background-color: color-mix(in srgb, var(--surface) 60%, transparent);
          border: 1px solid color-mix(in srgb, var(--border) 80%, transparent);
          padding: 8px 12px;
          border-radius: 0;
          font-size: 0.9rem;
          transition: all 0.2s;
          color: var(--text-main);
          box-sizing: border-box;
          width: 100%;
          font-family: 'JetBrains Mono', monospace;
        }

        .field-group input:focus {
          border-color: var(--primary);
          background-color: color-mix(in srgb, var(--primary) 10%, transparent);
          box-shadow: 0 0 10px color-mix(in srgb, var(--primary) 20%, transparent);
          outline: none;
        }

        /* Actions and terminal mockup block */
        .warp-actions-container {
          display: flex;
          flex-direction: column;
          gap: 20px;
        }

        .warp-buttons {
          display: flex;
          gap: 12px;
        }

        .warp-term-container {
          margin-top: 10px;
        }

        .warp-term-container .terminal-body {
          color: var(--primary) !important;
          text-shadow: 0 0 8px color-mix(in srgb, var(--primary) 40%, transparent);
        }
      </style>

      <div class="card warp-card">
        <div class="card-header-warp">
          <div class="warp-header-title">
            <h2>WARP</h2>
            <p class="hint">Configure the optional sing-box WireGuard/WARP sidecar through <code>/api/warp</code>.</p>
            <p class="hint redacted-note">Redacted private/license keys are preserved by the API when saved back as <code>[REDACTED]</code>.</p>
          </div>
        </div>

        <form id="warp-form">
          <!-- Status Row with Sliding Switch -->
          <div class="warp-status-row">
            <div class="warp-status-info">
              <span class="warp-status-label">WARP Integration Status</span>
              <span class="warp-status-sub">Enable or disable the background WireGuard/WARP service</span>
            </div>
            <label class="switch">
              <input id="warp-enabled" type="checkbox">
              <span class="slider round"></span>
            </label>
          </div>

          <!-- Fields Grid -->
          <div class="warp-grid">
            <!-- Connection Settings -->
            <div class="warp-field-section">
              <div class="section-title">
                <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M5 12.55a11 11 0 0 1 14.08 0"></path><path d="M1.42 9a16 16 0 0 1 21.16 0"></path><path d="M8.58 16.14a7 7 0 0 1 6.84 0"></path><line x1="12" y1="20" x2="12.01" y2="20"></line></svg>
                Connection settings
              </div>
              <div class="field-group">
                <label for="warp-endpoint">Endpoint</label>
                <input id="warp-endpoint" autocomplete="off" placeholder="engage.cloudflareclient.com:2408">
              </div>
              <div class="field-group">
                <label for="warp-local-address">Local address</label>
                <input id="warp-local-address" autocomplete="off" placeholder="172.16.0.2/32">
              </div>
              <div class="field-group">
                <label for="warp-mtu">MTU</label>
                <input id="warp-mtu" type="number" min="576" max="9000" placeholder="1280">
              </div>
            </div>

            <!-- Credentials -->
            <div class="warp-field-section">
              <div class="section-title">
                <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><rect x="3" y="11" width="18" height="11" rx="2" ry="2"></rect><path d="M7 11V7a5 5 0 0 1 10 0v4"></path></svg>
                Credentials
              </div>
              <div class="field-group">
                <label for="warp-private-key">Private key</label>
                <input id="warp-private-key" type="password" autocomplete="off" placeholder="WireGuard private key">
              </div>
              <div class="field-group">
                <label for="warp-peer-public-key">Peer public key</label>
                <input id="warp-peer-public-key" autocomplete="off" placeholder="Cloudflare peer public key">
              </div>
              <div class="field-group">
                <label for="warp-license-key">License key</label>
                <input id="warp-license-key" type="password" autocomplete="off" placeholder="Optional WARP+ license">
              </div>
            </div>

            <!-- Proxy & Routing -->
            <div class="warp-field-section">
              <div class="section-title">
                <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="3"></circle><path d="M19.4 15a1.65 1.65 0 0 0 .33 1.82l.06.06a2 2 0 1 1-2.83 2.83l-.06-.06a1.65 1.65 0 0 0-1.82-.33 1.65 1.65 0 0 0-1 1.51V21a2 2 0 0 1-4 0v-.09A1.65 1.65 0 0 0 9 19.4a1.65 1.65 0 0 0-1.82.33l-.06.06a2 2 0 1 1-2.83-2.83l.06-.06a1.65 1.65 0 0 0 .33-1.82 1.65 1.65 0 0 0-1.51-1H3a2 2 0 0 1 0-4h.09A1.65 1.65 0 0 0 4.6 9a1.65 1.65 0 0 0-.33-1.82l-.06-.06a2 2 0 1 1 2.83-2.83l.06.06a1.65 1.65 0 0 0 1.82.33H9a1.65 1.65 0 0 0 1-1.51V3a2 2 0 0 1 4 0v.09a1.65 1.65 0 0 0 1 1.51 1.65 1.65 0 0 0 1.82-.33l.06-.06a2 2 0 1 1 2.83 2.83l-.06.06a1.65 1.65 0 0 0-.33 1.82V9a1.65 1.65 0 0 0 1.51 1H21a2 2 0 0 1 0 4h-.09a1.65 1.65 0 0 0-1.51 1z"></path></svg>
                Proxy & Routing
              </div>
              <div class="field-group">
                <label for="warp-socks-listen">SOCKS listen</label>
                <input id="warp-socks-listen" autocomplete="off" placeholder="127.0.0.1">
              </div>
              <div class="field-group">
                <label for="warp-socks-port">SOCKS port</label>
                <input id="warp-socks-port" type="number" min="1" max="65535" placeholder="40000">
              </div>
              <div class="field-group">
                <label for="warp-reserved">Reserved bytes</label>
                <input id="warp-reserved" autocomplete="off" placeholder="1,2,3">
              </div>
            </div>
          </div>

          <!-- Actions & Terminal Section -->
          <div class="warp-actions-container">
            <div class="warp-buttons">
              <button id="save-warp-config" class="primary-btn" type="submit">
                <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M19 21H5a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h11l5 5v11a2 2 0 0 1-2 2z"></path><polyline points="17 21 17 13 7 13 7 21"></polyline><polyline points="7 3 7 8 15 8"></polyline></svg>
                Save WARP config
              </button>
              <button class="secondary-btn" id="load-warp-config" type="button">
                <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M21.5 2v6h-6M21.34 15.57a10 10 0 1 1-.57-8.38l5.67-5.67"></path></svg>
                Load WARP
              </button>
            </div>

            <!-- macOS style Terminal window mockup for WARP -->
            <div class="terminal-container warp-term-container">
              <div class="terminal-window">
                <div class="terminal-header">
                  <span class="terminal-title">warp-output</span>
                </div>
              <pre id="warp-output" class="terminal-body" role="status" aria-live="polite">Not loaded</pre>
              </div>
              <div class="terminal-actions">
                <button class="btn-console-clear" type="button" onclick="document.getElementById('warp-output').textContent = 'Console cleared.'">Clear Console</button>
              </div>
            </div>
          </div>
        </form>
      </div>`
}
