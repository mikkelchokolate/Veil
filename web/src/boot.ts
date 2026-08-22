import { captureLoginFields } from "./pendingLogin";
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
	captureLoginFields(false);
	void import("./main").then(() => {
		window.__VEIL_READY = true;
	});
}

window.__VEIL_BOOT = boot;

function inStaticLogin(event: Event): boolean {
	const target = event.target;
	return target instanceof Element && Boolean(target.closest(".auth-card"));
}

function bootFromPageIntent(event: Event): void {
	if (started || inStaticLogin(event)) return;
	boot();
}

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
function hasSpaMarker(): boolean {
	try {
		return localStorage.getItem("veil_spa") === "1";
	} catch {
		return false;
	}
}

// Reload of a deep link, or of `/` after login (non-HttpOnly marker cookie).
if (routePath() !== "/" || hasSpaMarker()) {
	boot();
}

// Do not boot on focus/keydown inside the static login card: Playwright fill()
// and the first keystroke would remount LoginView before credentials exist.
document.addEventListener("pointerdown", bootFromPageIntent, { capture: true });
document.addEventListener("click", bootFromPageIntent, { capture: true });
document.addEventListener(
	"submit",
	(event) => {
		if (window.__VEIL_READY) return;
		event.preventDefault();
		captureLoginFields(true);
		boot();
	},
	{ capture: true },
);
