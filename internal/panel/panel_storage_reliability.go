package panel

import "strings"

const panelStorageBridge = `<script>
  const veilStorageFallback = new Map();
  const veilStorage = {
    getItem(key) {
      try {
        const value = window.localStorage.getItem(key);
        if (value !== null) {
          veilStorageFallback.set(key, value);
          return value;
        }
      } catch (_) {}
      return veilStorageFallback.has(key) ? veilStorageFallback.get(key) : null;
    },
    setItem(key, value) {
      const text = String(value);
      veilStorageFallback.set(key, text);
      try { window.localStorage.setItem(key, text); } catch (_) {}
    },
    removeItem(key) {
      veilStorageFallback.delete(key);
      try { window.localStorage.removeItem(key); } catch (_) {}
    }
  };
</script>
`

// StorageReliableHTML keeps the Panel functional when a browser blocks or
// exhausts localStorage. Persistent storage remains the preferred cache, while
// an in-memory fallback preserves the current tab's auth and role state.
func StorageReliableHTML(html string) string {
	html = strings.ReplaceAll(html, "localStorage.getItem(", "veilStorage.getItem(")
	html = strings.ReplaceAll(html, "localStorage.setItem(", "veilStorage.setItem(")
	html = strings.ReplaceAll(html, "localStorage.removeItem(", "veilStorage.removeItem(")
	return strings.Replace(html, "</head>", panelStorageBridge+"</head>", 1)
}
