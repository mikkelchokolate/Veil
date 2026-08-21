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
