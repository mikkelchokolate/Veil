const HTML_ESCAPE: Record<string, string> = {
	"&": "&amp;",
	"<": "&lt;",
	">": "&gt;",
	'"': "&quot;",
	"'": "&#39;",
};

/**
 * Escape a string for interpolation into an HTML fragment.
 *
 * ECharts tooltip formatters return raw HTML, so any user-controlled value
 * (e.g. a client name that may contain HTML metacharacters) must be escaped
 * before it is concatenated into the formatter result.
 */
export function escapeHtml(value: string): string {
	return value.replace(/[&<>"']/g, (ch) => HTML_ESCAPE[ch] ?? ch);
}
