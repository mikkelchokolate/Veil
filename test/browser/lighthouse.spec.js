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
	const candidates = [];
	if (process.env.CHROME_PATH) candidates.push(process.env.CHROME_PATH);
	try {
		candidates.push(require("playwright-core").chromium.executablePath());
	} catch {
		try {
			candidates.push(require("@playwright/test").chromium.executablePath());
		} catch {
			/* fall through to the Playwright browser cache */
		}
	}
	const cache = process.env.PLAYWRIGHT_BROWSERS_PATH
		? process.env.PLAYWRIGHT_BROWSERS_PATH
		: path.join(os.homedir(), ".cache", "ms-playwright");
	if (fs.existsSync(cache)) {
		for (const dir of fs.readdirSync(cache)) {
			candidates.push(path.join(cache, dir, "chrome-linux64", "chrome"));
		}
	}
	for (const candidate of candidates) {
		if (candidate && fs.existsSync(candidate)) return candidate;
	}
	throw new Error(`no Chrome executable found (tried ${candidates.join(", ")})`);
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

function failedAudits(report) {
	const failed = [];
	for (const [id, cat] of Object.entries(report.categories)) {
		for (const ref of cat.auditRefs || []) {
			if (!ref.weight) continue;
			const audit = report.audits[ref.id];
			if (audit && audit.score !== null && audit.score < 1) {
				failed.push(`${id}:${ref.id}`);
			}
		}
	}
	return failed;
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
		const report1 = runLighthouse(`${url}/`, run1);
		const report2 = runLighthouse(`${url}/`, run2);
		const first = categoryScores(report1);
		const second = categoryScores(report2);
		const want = {
			performance: 100,
			accessibility: 100,
			"best-practices": 100,
			seo: 100,
		};
		expect(
			first,
			`run 1 ${JSON.stringify(first)} failed=${failedAudits(report1).join(",")}`,
		).toEqual(want);
		expect(
			second,
			`run 2 ${JSON.stringify(second)} failed=${failedAudits(report2).join(",")}`,
		).toEqual(want);
	});
});
