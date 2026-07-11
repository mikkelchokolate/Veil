package panel

import "strings"

const panelAuthenticationExpiryBridge = `<script>
  const veilNativeFetch = window.fetch.bind(window);
  let veilAuthenticationReloadScheduled = false;

  function scheduleVeilAuthenticationReset() {
    if (veilAuthenticationReloadScheduled) return;
    if (veilStorage.getItem('veil_api_token')) return;
    veilAuthenticationReloadScheduled = true;
    window.veil_csrf_token = '';
    window.veil_user_role = '';
    veilStorage.removeItem('veil_csrf_token');
    veilStorage.removeItem('veil_username');
    veilStorage.removeItem('veil_user_role');
    document.querySelectorAll('button, input, select, textarea').forEach((control) => {
      control.disabled = true;
    });
    window.setTimeout(() => window.location.reload(), 100);
  }

  window.fetch = async function(...args) {
    const response = await veilNativeFetch(...args);
    if (response && response.status === 401) {
      scheduleVeilAuthenticationReset();
    }
    return response;
  };
</script>
`

// AuthenticationExpiryReliableHTML makes an expired cookie session fail closed
// across every Panel request. Static-token failures remain editable so an
// operator can correct the token without being trapped in a reload loop.
func AuthenticationExpiryReliableHTML(html string) string {
	return strings.Replace(html, "</head>", panelAuthenticationExpiryBridge+"</head>", 1)
}
