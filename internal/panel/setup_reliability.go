package panel

import "strings"

// ReliableSetupHTML aligns browser-side first-run credential validation with
// the API contract. Native minlength/maxlength attributes count UTF-16 code
// units, while the backend constrains Unicode characters and UTF-8 bytes.
func ReliableSetupHTML(basePath string, locale string) string {
	html := SetupHTML(basePath, locale)
	html = strings.Replace(html,
		`<input id="setup-password" name="password" type="password" minlength="12" autocomplete="new-password" required>`,
		`<input id="setup-password" name="password" type="password" minlength="12" maxlength="72" autocomplete="new-password" required>`, 1)
	html = strings.Replace(html, `    let setupInFlight = false;`, `    function validSetupUsername(username) {
      const value = String(username || '');
      const byteLength = new TextEncoder().encode(value).length;
      return Array.from(value).length >= 3
        && byteLength <= 64
        && /^[\p{L}\p{N}._-]+$/u.test(value);
    }

    function validSetupPassword(password) {
      const value = String(password || '');
      return Array.from(value).length >= 12
        && new TextEncoder().encode(value).length <= 72;
    }

    const setupUsernameInput = document.getElementById('setup-username');
    const setupPasswordInput = document.getElementById('setup-password');
    setupUsernameInput.addEventListener('input', () => setupUsernameInput.setCustomValidity(''));
    setupPasswordInput.addEventListener('input', () => setupPasswordInput.setCustomValidity(''));

    let setupInFlight = false;`, 1)
	html = strings.Replace(html, `      event.preventDefault();
      if (setupInFlight) return;`, `      event.preventDefault();
      if (setupInFlight) return;
      const username = setupUsernameInput.value.trim();
      const password = setupPasswordInput.value;
      const validationOutput = document.getElementById('setup-result');
      if (!validSetupUsername(username)) {
        const message = 'Username must be 3-64 characters and at most 64 UTF-8 bytes, using letters, digits, dot, underscore, or hyphen.';
        setupUsernameInput.setCustomValidity(message);
        setupUsernameInput.reportValidity();
        validationOutput.className = 'error';
        validationOutput.textContent = message;
        return;
      }
      if (!validSetupPassword(password)) {
        const message = 'Password must be at least 12 characters and at most 72 UTF-8 bytes.';
        setupPasswordInput.setCustomValidity(message);
        setupPasswordInput.reportValidity();
        validationOutput.className = 'error';
        validationOutput.textContent = message;
        return;
      }`, 1)
	return html
}
