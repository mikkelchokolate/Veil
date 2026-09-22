const HTML_ESCAPE: Record<string, string> = {
	"&": "&amp;",
	"<": "&lt;",
	">": "&gt;",
	'"': "&quot;",
	"'": "&#39;",
};

/**
 * Escape a value for interpolation into an HTML fragment.
 *
 * ECharts tooltip formatters return raw HTML, so any user-controlled value
 * (e.g. a client name that may contain HTML metacharacters) must be escaped
 * before it is concatenated into the formatter result. Non-string input is
 * coerced rather than throwing — a broken tooltip is still a UI failure.
 */
export function escapeHtml(value: unknown): string {
	return String(value).replace(/[&<>"']/g, (ch) => HTML_ESCAPE[ch] ?? ch);
}
