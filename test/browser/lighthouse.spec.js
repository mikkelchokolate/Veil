// Gating Lighthouse run against the shipped production first-load (login).
// Drives the real Lighthouse CLI (default mobile + default categories).
const { spawnSync } = require("node:child_process");
const fs = require("node:fs");
const os = require("node:os");
const path = require("node:path");
const { test, expect } = require("@playwright/test");

function lighthouseBin() {
	const local = path.join(__dirname, "node_modules", ".bin", "lighthouse");
	if (fs.existsSync(local)) return local;
	return "lighthouse";
}

function chromePath() {
	if (process.env.CHROME_PATH) return process.env.CHROME_PATH;
	try {
		return require("playwright-core").chromium.executablePath();
	} catch {
		return require("@playwright/test").chromium.executablePath();
	}
}

function runLighthouse(url, outFile) {
	const chrome = chromePath();
	process.env.CHROME_PATH = chrome;
	const args = [
		url,
		"--chrome-path",
		chrome,
		"--chrome-flags=--headless=new --no-sandbox --disable-dev-shm-usage --disable-gpu",
		"--only-categories=performance,accessibility,best-practices,seo",
		"--form-factor=mobile",
		"--screenEmulation.mobile",
		"--disable-full-page-screenshot",
		"--no-enable-error-reporting",
		"--quiet",
		"--output=json",
		`--output-path=${outFile}`,
	];
	const result = spawnSync(lighthouseBin(), args, {
		encoding: "utf8",
		timeout: 120_000,
		env: { ...process.env, CHROME_PATH: chromePath() },
	});
	if (result.status !== 0) {
		const err = (result.stderr || result.stdout || "").slice(-4000);
		throw new Error(`lighthouse exited ${result.status}: ${err}`);
	}
	return JSON.parse(fs.readFileSync(outFile, "utf8"));
}

function categoryScores(report) {
	const cats = report.categories;
	return {
		performance: Math.round((cats.performance.score || 0) * 100),
		accessibility: Math.round((cats.accessibility.score || 0) * 100),
		"best-practices": Math.round((cats["best-practices"].score || 0) * 100),
		seo: Math.round((cats.seo.score || 0) * 100),
	};
}

test.describe("Lighthouse first-load", () => {
	test.describe.configure({ timeout: 240_000 });
	test("scores 100 in Performance, Accessibility, Best Practices, and SEO twice", () => {
		const url = (
			process.env.VEIL_LH_URL ||
			process.env.VEIL_BROWSER_BASE_URL ||
			"http://127.0.0.1:4173"
		).replace(/\/+$/, "");
		const outDir =
			process.env.VEIL_LH_OUT_DIR ||
			fs.mkdtempSync(path.join(os.tmpdir(), "veil-lh-"));
		fs.mkdirSync(outDir, { recursive: true });
		const run1 = path.join(outDir, "lighthouse-run-1.json");
		const run2 = path.join(outDir, "lighthouse-run-2.json");
		const first = categoryScores(runLighthouse(`${url}/`, run1));
		const second = categoryScores(runLighthouse(`${url}/`, run2));
		expect(first, `run 1 ${JSON.stringify(first)}`).toEqual({
			performance: 100,
			accessibility: 100,
			"best-practices": 100,
			seo: 100,
		});
		expect(second, `run 2 ${JSON.stringify(second)}`).toEqual({
			performance: 100,
			accessibility: 100,
			"best-practices": 100,
			seo: 100,
		});
	});
});
