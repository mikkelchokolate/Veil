package panel

import "strings"

// ReliableLoginHTML keeps a successful cookie login authoritative even when
// browser storage is unavailable or full. Session metadata is only a cache for
// the Panel; failure to persist it must not trap the user on the login form.
func ReliableLoginHTML(basePath string, locale string) string {
	html := LoginHTML(basePath, locale)
	return strings.Replace(html, `        authenticated = true;
        localStorage.setItem('veil_csrf_token', data.csrfToken);
        localStorage.setItem('veil_username', data.username);
        localStorage.setItem('veil_user_role', data.role || '');
        localStorage.removeItem('veil_api_token');
        if (data.locale) {
          document.cookie = 'veil_locale=' + encodeURIComponent(data.locale) + '; Path=/; Max-Age=31536000; SameSite=Lax';
        }`, `        authenticated = true;
        try {
          localStorage.setItem('veil_csrf_token', data.csrfToken);
          localStorage.setItem('veil_username', data.username);
          localStorage.setItem('veil_user_role', data.role || '');
          localStorage.removeItem('veil_api_token');
        } catch (storageError) {
          console.warn('Could not persist login metadata.', storageError);
        }
        if (data.locale) {
          try {
            document.cookie = 'veil_locale=' + encodeURIComponent(data.locale) + '; Path=/; Max-Age=31536000; SameSite=Lax';
          } catch (cookieError) {
            console.warn('Could not persist login locale.', cookieError);
          }
        }`, 1)
}
