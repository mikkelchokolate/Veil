package panel

const panelClientProfileControlsPlaceholder = "__VEIL_PANEL_CLIENT_PROFILE_CONTROLS__"
const panelClientProfileActionsPlaceholder = "__VEIL_PANEL_CLIENT_PROFILE_ACTIONS__"

// panelClientProfileControlsHTML renders the Panel controls for editing Client profiles
// attached to an Inbound. Keeping this Module separate gives the Client profile UI a
// small Interface instead of requiring tests to search the full Panel HTML.
func panelClientProfileControlsHTML() string {
	return `              <label>Client profile</label>
              <div class="form-grid">
                <input id="client-profile-name" autocomplete="off" placeholder="profile name, e.g. alice">
                <input id="client-profile-username" autocomplete="off" placeholder="username (optional)">
                <div style="display:flex;gap:8px">
                  <input id="client-profile-password" type="text" autocomplete="off" placeholder="password" style="flex:1">
                  <button type="button" class="secondary" onclick="genClientProfilePassword()" style="white-space:nowrap">Generate</button>
                </div>
                <button type="button" class="secondary" onclick="addClientProfile()">Add profile</button>
                <button type="button" class="secondary" onclick="generateAndAddProfile()" style="grid-column: 1 / -1; width: 100%;">Generate profile</button>
              </div>
              <label for="inbound-profiles">Client profiles (JSON)</label>
              <textarea id="inbound-profiles" rows="4" spellcheck="false" placeholder='[{"name":"alice","username":"alice","password":"optional","enabled":true}]'></textarea>
              <p class="hint">Use Add profile for multiple users on one Inbound, or edit JSON directly.</p>`
}

// panelClientProfileActionsJS renders the browser-side actions for the Client profile
// controls. The behavior remains intentionally small and colocated with the controls.
func panelClientProfileActionsJS() string {
	return `    function genClientProfilePassword() {
      document.getElementById('client-profile-password').value = randomPassword();
    }

    function showClientProfileError(message) {
      if (typeof veilShowInboundLocalError === 'function') {
        veilShowInboundLocalError(message);
        return;
      }
      const summary = document.getElementById('inbound-validation-summary');
      if (summary) {
        summary.className = 'validation-summary validation-error';
        summary.textContent = String(message);
      }
    }

    function readClientProfiles() {
      const raw = document.getElementById('inbound-profiles').value.trim();
      if (!raw) return [];
      const profiles = JSON.parse(raw);
      if (!Array.isArray(profiles)) {
        throw new Error('Client profiles JSON must be an array.');
      }
      return profiles;
    }

    function writeClientProfiles(profiles) {
      document.getElementById('inbound-profiles').value = JSON.stringify(profiles, null, 2);
      scheduleInboundValidation();
    }

    function generateAndAddProfile() {
      let profiles;
      try {
        profiles = readClientProfiles();
      } catch (err) {
        showClientProfileError(veilT('inbounds.profilesInvalid', { error: String(err) }));
        return;
      }
      let name;
      do {
        name = 'client_' + randomPassword().slice(0, 7).replace(/[^a-z0-9]/gi, '').toLowerCase();
      } while (profiles.some((profile) => profile && profile.name === name));
      profiles.push({
        name: name,
        username: name,
        password: randomPassword(),
        enabled: true
      });
      writeClientProfiles(profiles);
    }

    function addClientProfile() {
      const name = document.getElementById('client-profile-name').value.trim();
      if (!name) {
        showClientProfileError(veilT('inbounds.profileNameRequired'));
        return;
      }
      let profiles;
      try {
        profiles = readClientProfiles();
      } catch (err) {
        showClientProfileError(veilT('inbounds.profilesInvalid', { error: String(err) }));
        return;
      }
      if (profiles.some((profile) => profile && profile.name === name)) {
        showClientProfileError('A client profile with this name already exists.');
        return;
      }
      const username = document.getElementById('client-profile-username').value.trim();
      let password = document.getElementById('client-profile-password').value.trim();
      if (!password) password = randomPassword();
      profiles.push({
        name: name,
        username: username || undefined,
        password: password,
        enabled: true
      });
      writeClientProfiles(profiles);
      document.getElementById('client-profile-name').value = '';
      document.getElementById('client-profile-username').value = '';
      document.getElementById('client-profile-password').value = '';
    }` + panelInboundReliabilityJS()
}
