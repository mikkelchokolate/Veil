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
      <p id="apply-runtime-output">Runtime units: not planned</p>
      <pre id="apply-plan-output">Not planned</pre>
    </div>`
}
