package panel

const panelApplyCardPlaceholder = "__VEIL_PANEL_APPLY_CARD__"

func panelApplyCardHTML() string {
	return `    <div class="card">
      <h2>Apply plan</h2>
      <p>Validate current management state and show staged config/reload actions before any real service changes: <code>/api/apply/plan</code></p>
      <p>Service reload also runs fixed health checks and automatically rolls live configs back if reload or health fails.</p>
      <button id="build-apply-plan" type="button">Build apply plan</button>
      <button id="apply-staged-files" type="button">Apply staged files</button>
      <button id="apply-live-configs" type="button">Apply live configs</button>
      <button id="reload-services" type="button">Reload and health check services</button>
      <h3>Safe apply preview</h3>
      <p class="hint">Shows file-level config changes, runtime actions, and DNS/TLS/firewall warnings without exposing generated config contents or secrets.</p>
      <p id="apply-safety-warnings" class="hint" role="status" aria-live="polite">Safety warnings: build a plan first</p>
      <div class="table-container">
        <table>
          <thead>
            <tr>
              <th>Operation</th>
              <th>Target</th>
              <th>Interruption risk</th>
              <th>Rollback</th>
              <th>Validated by</th>
            </tr>
          </thead>
          <tbody id="apply-file-diff-preview-body">
            <tr>
              <td colspan="5">Build a plan to preview managed files and runtime actions.</td>
            </tr>
          </tbody>
        </table>
      </div>
      <div class="form-grid">
        <div>
          <label for="apply-history-stage">History stage</label>
          <select id="apply-history-stage">
            <option value="">all</option>
            <option value="staged">staged</option>
            <option value="live">live</option>
            <option value="services">services</option>
            <option value="validation">validation</option>
            <option value="rollback">rollback</option>
          </select>
        </div>
        <div>
          <label for="apply-history-success">History success</label>
          <select id="apply-history-success">
            <option value="">all</option>
            <option value="true">success</option>
            <option value="false">failed</option>
          </select>
        </div>
        <div>
          <label for="apply-history-limit">History limit</label>
          <input id="apply-history-limit" type="number" min="0" placeholder="50">
        </div>
      </div>
      <button id="load-apply-history" type="button">Load apply history</button>
      <p id="apply-runtime-output" role="status" aria-live="polite">Runtime units: not planned</p>
      <pre id="apply-plan-output" role="status" aria-live="polite">Not planned</pre>
    </div>`
}
