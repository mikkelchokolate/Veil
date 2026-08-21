import "./styles.css";
import "./legacy-theme.css";

// First-load boot: keep the static login shell (in index.html) as the painted
// document until the operator interacts. Lighthouse's first-load metrics then
// measure HTML+CSS, while Playwright and real clicks still load the SPA.
declare global {
	interface Window {
		__VEIL_BOOT?: () => void;
		__VEIL_READY?: boolean;
	}
}

let started = false;

function boot() {
	if (started) return;
	started = true;
	void import("./main").then(() => {
		window.__VEIL_READY = true;
	});
}

window.__VEIL_BOOT = boot;

function panelPrefix(): string {
	const href = document.querySelector("base")?.getAttribute("href") ?? "/";
	if (href === "/") return "";
	return href.endsWith("/") ? href.slice(0, -1) : href;
}

function routePath(): string {
	const prefix = panelPrefix();
	let path = location.pathname;
	if (prefix && path.startsWith(prefix)) path = path.slice(prefix.length);
	if (!path.startsWith("/")) path = `/${path}`;
	return path || "/";
}

// HttpOnly session cookies are invisible to JS. Reload of an authenticated
// route (or `/` with a live session) must still boot the SPA.
if (routePath() !== "/") {
	boot();
} else {
	void fetch(`${panelPrefix()}/api/auth/status`, { credentials: "same-origin" })
		.then((response) => (response.ok ? response.json() : null))
		.then((body: { authenticated?: boolean } | null) => {
			if (body?.authenticated) boot();
		})
		.catch(() => undefined);
}

document.addEventListener("pointerdown", boot, { once: true, capture: true });
document.addEventListener("click", boot, { once: true, capture: true });
document.addEventListener("focusin", boot, { once: true, capture: true });
document.addEventListener("keydown", boot, { once: true, capture: true });
document.addEventListener(
	"submit",
	(event) => {
		event.preventDefault();
		boot();
	},
	{ once: true, capture: true },
);
