package panel

const panelRoutingCardPlaceholder = "__VEIL_PANEL_ROUTING_CARD__"

func panelRoutingCardHTML() string {
	return `      <style>
        /* Premium Routing Rules Card Styles */
        .routing-card {
          background: color-mix(in srgb, var(--surface) 40%, transparent) !important;
          backdrop-filter: blur(12px);
          -webkit-backdrop-filter: blur(12px);
          border: 1px solid var(--border) !important;
          position: relative;
          overflow: hidden;
          transition: border-color 0.2s ease;
        }
        .routing-card:hover {
          border-color: var(--border-hover) !important;
        }

        .card-header {
          display: flex;
          justify-content: space-between;
          align-items: center;
          border-bottom: 1px solid var(--border);
          padding-bottom: 20px;
          margin-bottom: 20px;
          flex-wrap: wrap;
          gap: 16px;
        }

        .header-title h2 {
          margin: 0 0 6px 0 !important;
          font-size: 1.1rem;
          font-family: 'JetBrains Mono', monospace;
          text-transform: uppercase;
          letter-spacing: 0.08rem;
          color: #fff;
          mix-blend-mode: plus-lighter;
        }

        .header-title .hint {
          margin: 0;
          color: var(--text-muted);
          font-size: 0.85rem;
        }

        .header-actions {
          display: flex;
          gap: 10px;
        }

        /* Premium Buttons are styled globally */

        /* Elegant Table Design */
        .table-responsive {
          width: 100%;
          overflow-x: auto;
          border-radius: 0;
          border: 1px solid var(--border);
          background: color-mix(in srgb, var(--surface) 40%, transparent);
          backdrop-filter: blur(12px);
          -webkit-backdrop-filter: blur(12px);
          margin-bottom: 24px;
        }

        .premium-table {
          width: 100%;
          border-collapse: collapse;
          text-align: left;
          font-size: 0.9rem;
        }

        .premium-table th {
          background: color-mix(in srgb, var(--canvas) 60%, transparent);
          padding: 14px 18px;
          color: var(--text-muted);
          font-family: 'JetBrains Mono', monospace;
          font-weight: 600;
          text-transform: uppercase;
          font-size: 0.8rem;
          letter-spacing: 0.05rem;
          border-bottom: 1px solid var(--border);
        }

        .premium-table td {
          padding: 16px 18px;
          border-bottom: 1px solid var(--border);
          color: var(--text-main);
        }

        .premium-table tr:last-child td {
          border-bottom: none;
        }

        .premium-table tr:hover td {
          background: var(--bg-hover);
        }

        .font-semibold {
          font-weight: 600;
          color: #fff;
        }

        .match-badge {
          background: color-mix(in srgb, var(--canvas) 60%, transparent) !important;
          color: var(--primary) !important;
          border: 1px solid var(--border) !important;
          padding: 4px 8px !important;
          border-radius: 0 !important;
          font-family: 'JetBrains Mono', monospace !important;
          font-size: 0.8rem !important;
        }

        .outbound-badge {
          display: inline-block;
          padding: 4px 10px;
          border-radius: 0;
          font-size: 0.75rem;
          font-family: 'JetBrains Mono', monospace;
          font-weight: 700;
          text-transform: uppercase;
          letter-spacing: 0.05rem;
        }

        .badge-direct {
          background: color-mix(in srgb, var(--accent-success) 10%, transparent);
          color: var(--accent-success);
          border: 1px solid color-mix(in srgb, var(--accent-success) 30%, transparent);
        }

        .badge-proxy {
          background: color-mix(in srgb, var(--accent) 10%, transparent);
          color: var(--accent);
          border: 1px solid color-mix(in srgb, var(--accent) 30%, transparent);
        }

        .badge-warp {
          background: color-mix(in srgb, var(--accent-warning) 10%, transparent);
          color: var(--accent-warning);
          border: 1px solid color-mix(in srgb, var(--accent-warning) 30%, transparent);
        }

        .btn-action-edit {
          background: transparent !important;
          border: 1px solid var(--border) !important;
          color: var(--text-muted) !important;
          padding: 6px 14px !important;
          border-radius: 0 !important;
          font-family: 'JetBrains Mono', monospace !important;
          font-size: 0.8rem !important;
          font-weight: 500 !important;
          text-transform: uppercase;
          letter-spacing: 0.03rem;
          cursor: pointer;
          transition: all 0.2s ease !important;
        }

        .btn-action-edit:hover {
          background: var(--bg-hover) !important;
          border-color: var(--primary) !important;
          color: white !important;
        }

        .empty-state {
          text-align: center;
          color: var(--text-muted);
          font-style: italic;
          padding: 32px !important;
        }

        /* Custom Toggle Switch Styling */
        .switch {
          position: relative;
          display: inline-block !important;
          margin: 0 !important;
          width: 44px;
          height: 24px;
          vertical-align: middle !important;
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
          transition: .3s;
          border: 1px solid var(--border);
        }

        .slider:before {
          position: absolute;
          content: '';
          height: 14px;
          width: 14px;
          left: 4px;
          bottom: 4px;
          background-color: var(--text-muted);
          transition: .3s;
        }

        input:checked + .slider {
          background-color: color-mix(in srgb, var(--accent-success) 15%, transparent);
          border-color: var(--accent-success);
        }

        input:checked + .slider:before {
          background-color: var(--accent-success);
          transform: translateX(20px);
        }

        .slider.round {
          border-radius: 0;
        }

        .slider.round:before {
          border-radius: 0;
        }

        .small-switch {
          width: 36px;
          height: 20px;
          vertical-align: middle !important;
        }

        .small-switch .slider:before {
          height: 10px;
          width: 10px;
          left: 4px;
          bottom: 4px;
        }

        .small-switch input:checked + .slider:before {
          transform: translateX(16px);
        }

        /* Elegant Modal Design */
        .modal-overlay {
          position: fixed;
          top: 0;
          left: 0;
          right: 0;
          bottom: 0;
          background: rgba(6, 13, 13, 0.6);
          backdrop-filter: blur(8px);
          -webkit-backdrop-filter: blur(8px);
          display: flex;
          align-items: center;
          justify-content: center;
          z-index: 1000;
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
          width: 100%;
          max-width: 500px;
          padding: 24px;
          box-shadow: 0 25px 50px -12px rgba(0, 0, 0, 0.5);
          transform: scale(0.95);
          transition: transform 0.3s ease;
        }

        .modal-overlay.active .modal-content {
          transform: scale(1);
        }

        .modal-header {
          display: flex;
          justify-content: space-between;
          align-items: center;
          margin-bottom: 20px;
          border-bottom: 1px solid var(--border);
          padding-bottom: 12px;
        }

        .modal-header h3 {
          margin: 0;
          font-size: 1.1rem;
          font-family: 'JetBrains Mono', monospace;
          text-transform: uppercase;
          letter-spacing: 0.08rem;
          color: white;
          mix-blend-mode: plus-lighter;
        }

        .modal-close {
          background: transparent !important;
          border: 1px solid var(--border) !important;
          border-radius: 0;
          color: var(--text-muted);
          font-size: 1rem;
          font-family: 'JetBrains Mono', monospace;
          cursor: pointer;
          padding: 6px 12px !important;
          line-height: 1;
          transition: all 0.2s ease;
        }

        .modal-close:hover {
          background: var(--bg-hover) !important;
          border-color: var(--primary) !important;
          color: white;
        }

        .form-group {
          margin-bottom: 18px;
        }

        .form-group label {
          display: block;
          margin-bottom: 8px;
          font-size: 0.75rem;
          font-family: 'JetBrains Mono', monospace;
          text-transform: uppercase;
          letter-spacing: 0.05rem;
          color: var(--text-muted);
          font-weight: 500;
        }

        .form-group input, .form-group select {
          box-sizing: border-box;
          width: 100%;
          border: 1px solid var(--border);
          border-radius: 0;
          padding: 10px 12px;
          background: color-mix(in srgb, var(--canvas) 60%, transparent);
          backdrop-filter: blur(8px);
          -webkit-backdrop-filter: blur(8px);
          color: var(--text-main);
          font-family: inherit;
        }

        .form-group input:focus, .form-group select:focus {
          border-color: var(--primary);
          outline: none;
        }

        .form-group.inline-group {
          display: flex;
          justify-content: space-between;
          align-items: center;
          background: color-mix(in srgb, var(--canvas) 60%, transparent);
          backdrop-filter: blur(8px);
          -webkit-backdrop-filter: blur(8px);
          padding: 12px 16px;
          border-radius: 0;
          border: 1px solid var(--border);
        }

        .form-group.inline-group label {
          margin-bottom: 0;
        }

        .modal-footer {
          display: flex;
          justify-content: space-between;
          margin-top: 24px;
          padding-top: 16px;
          border-top: 1px solid var(--border);
          gap: 12px;
        }



        /* Tools Grid (Presets & Terminal) */
        .tools-grid {
          display: grid;
          grid-template-columns: 1fr 1fr;
          gap: 20px;
          margin-top: 24px;
        }

        @media (max-width: 768px) {
          .tools-grid {
            grid-template-columns: 1fr;
          }
        }

        .preset-card {
          background: color-mix(in srgb, var(--surface) 40%, transparent);
          backdrop-filter: blur(12px);
          -webkit-backdrop-filter: blur(12px);
          border: 1px solid var(--border);
          border-radius: 0;
          padding: 20px;
          display: flex;
          flex-direction: column;
          justify-content: space-between;
        }

        .preset-header {
          display: flex;
          align-items: center;
          gap: 8px;
          margin-bottom: 16px;
          color: var(--primary);
        }

        .preset-header h3 {
          margin: 0;
          font-size: 1rem;
          font-family: 'JetBrains Mono', monospace;
          text-transform: uppercase;
          letter-spacing: 0.05rem;
          color: white;
        }

        .preset-controls {
          display: flex;
          gap: 10px;
          margin-bottom: 14px;
        }

        .select-wrapper {
          position: relative;
          flex: 1;
        }

        .select-wrapper select {
          width: 100%;
        }



        /* macOS Terminal Mockup */
        .terminal-container {
          display: flex;
          flex-direction: column;
        }

        .terminal-window {
          background: rgba(2, 4, 4, 0.6);
          backdrop-filter: blur(10px);
          -webkit-backdrop-filter: blur(10px);
          border: 1px solid var(--border);
          border-radius: 0;
          overflow: hidden;
          display: flex;
          flex-direction: column;
          flex-grow: 1;
          min-height: 180px;
        }

        .terminal-header {
          background: color-mix(in srgb, var(--surface) 50%, transparent);
          backdrop-filter: blur(10px);
          -webkit-backdrop-filter: blur(10px);
          padding: 8px 16px;
          display: flex;
          align-items: center;
          border-bottom: 1px solid var(--border);
        }
        .terminal-title {
          color: var(--text-muted);
          font-size: 0.75rem;
          font-family: 'JetBrains Mono', monospace;
          text-transform: uppercase;
          letter-spacing: 0.05rem;
          margin: 0 auto;
        }

        .terminal-body {
          margin: 0 !important;
          background: rgba(2, 4, 4, 0.6) !important;
          backdrop-filter: blur(10px);
          -webkit-backdrop-filter: blur(10px);
          color: var(--primary) !important;
          font-family: 'JetBrains Mono', monospace !important;
          font-size: 0.85rem !important;
          padding: 16px !important;
          overflow: auto;
          flex-grow: 1;
          border-radius: 0 !important;
          min-height: 120px;
        }

        .terminal-actions {
          display: flex;
          justify-content: flex-end;
          gap: 8px;
          margin-top: 8px;
        }

        .btn-console-clear, .btn-console-load {
          background: transparent !important;
          border: none !important;
          color: var(--text-muted) !important;
          font-family: 'JetBrains Mono', monospace;
          text-transform: uppercase;
          letter-spacing: 0.05rem;
          font-size: 0.75rem !important;
          padding: 4px 8px !important;
          cursor: pointer;
        }

        .btn-console-clear:hover, .btn-console-load:hover {
          color: var(--text-main) !important;
        }
      </style>

      <div class="card routing-card">
        <div class="card-header">
          <div class="header-title">
            <h2>Routing rules</h2>
            <p class="hint">List, create, update, or delete routing rules through <code>/api/routing/rules</code>.</p>
          </div>
          <div class="header-actions">
            <button id="add-routing-rule-btn" type="button" onclick="openRoutingModal(null)">
              <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><line x1="12" y1="5" x2="12" y2="19"></line><line x1="5" y1="12" x2="19" y2="12"></line></svg>
              Add Rule
            </button>
            <button class="secondary" type="button" onclick="loadRoutingRules()">
              <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M21.5 2v6h-6M21.34 15.57a10 10 0 1 1-.57-8.38l5.67-5.67"></path></svg>
              Refresh
            </button>
          </div>
        </div>

        <!-- Routing Rules Table -->
        <div class="table-responsive">
          <table class="premium-table" id="routing-rules-table">
            <thead>
              <tr>
                <th>Name</th>
                <th>Match Rule</th>
                <th>Outbound</th>
                <th>Status</th>
                <th>Actions</th>
              </tr>
            </thead>
            <tbody id="routing-rules-tbody">
              <tr>
                <td colspan="5" class="empty-state">Click 'Refresh' or load to display routing rules.</td>
              </tr>
            </tbody>
          </table>
        </div>

        <!-- Presets and output -->
        <div class="tools-grid">
          <div class="preset-card">
            <div>
              <div class="preset-header">
                <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M4 15s1-1 4-1 5 2 8 2 4-1 4-1V3s-1 1-4 1-5-2-8-2-4 1-4 1z"></path><line x1="4" y1="22" x2="4" y2="15"></line></svg>
                <h3>Preset selectors</h3>
              </div>
              <div class="preset-controls">
                <div class="select-wrapper">
                  <select id="routing-preset-profile">
                    <option value="all">all</option>
                    <option value="all-except-Russia">all-except-Russia</option>
                    <option value="RU-blocked">RU-blocked</option>
                  </select>
                </div>
                <button id="apply-routing-preset" type="button">Apply routing preset</button>
              </div>
            </div>
            <div>
              <p class="hint">Russian geo/site data is pulled from <code>runetfreedom/russia-v2ray-rules-dat</code> when a Russia-aware preset is applied.</p>
            </div>
          </div>

          <div class="terminal-container">
            <div class="terminal-window">
              <div class="terminal-header">
                <span class="terminal-title">routing-output</span>
              </div>
              <pre id="routing-output" class="terminal-body">Not loaded</pre>
            </div>
            <div class="terminal-actions">
              <button class="btn-console-clear" type="button" onclick="document.getElementById('routing-output').textContent = 'Console cleared.'">Clear Console</button>
              <button class="btn-console-load" type="button" data-load="/api/routing/rules" data-output="routing-output">Load Raw API Response</button>
              <button class="btn-console-load" type="button" data-load="/api/routing/presets" data-output="routing-output">Load Presets API</button>
            </div>
          </div>
        </div>

        <!-- Modal Overlay for Add/Edit Form -->
        <div id="routing-modal" class="modal-overlay" onclick="if(event.target === this) closeRoutingModal()">
          <div class="modal-content">
            <div class="modal-header">
              <h3 id="modal-title">Add Routing Rule</h3>
              <button type="button" class="modal-close" onclick="closeRoutingModal()">&times;</button>
            </div>
            <form id="routing-rule-form">
              <div class="form-group">
                <label for="routing-rule-name">Name</label>
                <input id="routing-rule-name" autocomplete="off" placeholder="non-ru-through-warp" required>
              </div>
              <div class="form-group">
                <label for="routing-rule-match">Match</label>
                <input id="routing-rule-match" autocomplete="off" placeholder="geosite:geolocation-!ru">
              </div>
              <div class="form-group">
                <label for="routing-rule-outbound">Outbound</label>
                <div class="select-wrapper">
                  <select id="routing-rule-outbound">
                    <option value="direct">direct</option>
                    <option value="proxy">proxy</option>
                  </select>
                </div>
              </div>
              <div class="form-group inline-group">
                <label for="routing-rule-enabled">Enabled</label>
                <label class="switch small-switch">
                  <input id="routing-rule-enabled" type="checkbox" checked>
                  <span class="slider round"></span>
                </label>
              </div>
              <div class="modal-footer">
                <button id="delete-routing-rule" class="danger" type="button">Delete routing rule</button>
                <button id="save-routing-rule" type="submit">Save routing rule</button>
              </div>
            </form>
          </div>
        </div>
      </div>`
}
