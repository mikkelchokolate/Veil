package panel

// panelLocalePersistenceReliabilityJS intercepts locale changes before the
// generic localization runtime can optimistically update the cookie. Cookie
// sessions must persist the locale successfully before the UI reloads; otherwise
// the browser and stored user profile silently diverge.
func panelLocalePersistenceReliabilityJS() string {
	return `
    let panelLocaleChangeInFlight = false;
    document.addEventListener('change', async (event) => {
      const select = event.target;
      if (!select || !select.matches || !select.matches('[data-veil-locale-select]')) return;

      // The localization runtime also installs a target listener. Capture this
      // event first so only the failure-aware persistence path runs.
      event.preventDefault();
      event.stopImmediatePropagation();

      const nextLocale = String(select.value || '');
      if (!['en', 'ru'].includes(nextLocale) || nextLocale === window.veilLocale) return;
      if (panelLocaleChangeInFlight) {
        select.value = window.veilLocale;
        return;
      }

      panelLocaleChangeInFlight = true;
      select.disabled = true;
      const csrf = window.veil_csrf_token || localStorage.getItem('veil_csrf_token') || '';
      try {
        if (csrf) {
          const response = await fetch('/api/auth/locale', {
            method: 'POST',
            credentials: 'same-origin',
            headers: {
              'Content-Type': 'application/json',
              'X-CSRF-Token': csrf
            },
            body: JSON.stringify({ locale: nextLocale })
          });
          const text = await response.text();
          if (!response.ok) {
            throw new Error(formatAPIError(text, response.status));
          }
        }

        document.cookie = 'veil_locale=' + encodeURIComponent(nextLocale) + '; Path=/; Max-Age=31536000; SameSite=Lax';
        window.location.reload();
      } catch (error) {
        panelLocaleChangeInFlight = false;
        select.disabled = false;
        select.value = window.veilLocale;
        alert(veilT('status.requestFailed', {
          error: String(error && error.message ? error.message : error)
        }));
      }
    }, true);
`
}
