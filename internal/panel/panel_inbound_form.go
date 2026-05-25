package panel

import "github.com/veil-panel/veil/internal/protocols"

const panelInboundFormPlaceholder = "__VEIL_PANEL_INBOUND_FORM__"

// panelInboundFormHTML renders the Panel Module slice for Inbound management.
// Client profile controls are kept behind their own placeholder so the nested
// Module can evolve without forcing callers to understand the whole Inbound form.
func panelInboundFormHTML() string {
	return `      <!-- Inbounds Table Section (Main View) -->
      <div class="card">
        <div style="display: flex; justify-content: space-between; align-items: center; margin-bottom: 20px;">
          <h2>Inbounds</h2>
          <button type="button" onclick="openAddInboundModal()" style="display: flex; align-items: center; gap: 8px;">
            <span style="font-size: 1.2rem; font-weight: bold;">+</span> Add Inbound
          </button>
        </div>
        <p>Create, update, or delete ` + protocols.NewCatalog().DisplayNameList() + ` inbound definitions through <code>/api/inbounds</code>.</p>
        
        <!-- Table container -->
        <div class="table-container">
          <table id="inbounds-table">
            <thead>
              <tr>
                <th>Name</th>
                <th>Protocol</th>
                <th>Port</th>
                <th>Clients</th>
                <th>Status</th>
                <th style="width: 100px; text-align: center;">Actions</th>
              </tr>
            </thead>
            <tbody id="inbounds-tbody">
              <tr>
                <td colspan="6" style="text-align: center; color: var(--text-muted); padding: 24px;">Loading inbounds...</td>
              </tr>
            </tbody>
          </table>
        </div>
        
        <div style="margin-top: 16px; display: flex; gap: 8px; align-items: center;">
          <button class="secondary" id="load-inbounds" type="button">Refresh List</button>
          <button class="secondary" onclick="toggleRawView('inbounds-output')" type="button">Raw JSON</button>
        </div>
        <pre id="inbounds-output" style="display: none; margin-top: 16px;">Not loaded</pre>
      </div>

      <!-- Add/Edit Inbound Overlay Modal -->
      <div id="inbound-modal-overlay" class="modal-overlay">
        <div class="modal-content">
          <div class="modal-header">
            <h2 id="inbound-modal-title">Add Inbound</h2>
            <button type="button" class="modal-close" onclick="closeInboundModal()">&times;</button>
          </div>
          
          <form id="inbound-form">
            <div class="form-grid">
              <div>
                <label for="inbound-name">Name</label>
                <input id="inbound-name" autocomplete="off" placeholder="naive">
              </div>
              <div>
                <label for="inbound-protocol">Protocol</label>
                <select id="inbound-protocol">
` + panelInboundProtocolOptionsHTML() + `                </select>
              </div>
              <div>
                <label for="inbound-transport">Transport</label>
                <select id="inbound-transport">
` + panelInboundTransportOptionsHTML() + `                </select>
              </div>
              <div>
                <label for="inbound-port">Port</label>
                <input id="inbound-port" type="number" min="1" max="65535" placeholder="443">
              </div>
              <div>
                <label for="inbound-password">Password</label>
                <div style="display:flex;gap:8px">
                  <input id="inbound-password" type="text" autocomplete="off" placeholder="password" style="flex:1">
                  <button type="button" class="secondary" onclick="genInboundPassword()" style="white-space:nowrap; padding: 12px 14px;">Generate</button>
                </div>
              </div>
              <div>
                <label for="inbound-enabled">Status</label>
                <div style="display: flex; align-items: center; height: 46px;">
                  <label class="switch">
                    <input id="inbound-enabled" type="checkbox" checked>
                    <span class="slider"></span>
                  </label>
                  <span style="margin-left: 12px; font-weight: 500; font-size: 0.95rem;">Enabled</span>
                </div>
              </div>
              <div id="inbound-protocol-fields" style="grid-column: 1 / -1; display: flex; flex-direction: column; gap: 16px; border-top: 1px solid var(--border); padding-top: 16px; margin-top: 8px;"></div>
              <div style="grid-column: 1 / -1">
` + panelClientProfileControlsPlaceholder + `              </div>
            </div>
            
            <div class="actions" style="margin-top: 24px; display: flex; justify-content: flex-end; gap: 12px;">
              <button id="delete-inbound" class="danger" type="button" style="margin-right: auto; display: none;">Delete</button>
              <button class="secondary" type="button" onclick="closeInboundModal()">Cancel</button>
              <button id="save-inbound" type="submit">Save</button>
            </div>
          </form>
        </div>
      </div>`
}
