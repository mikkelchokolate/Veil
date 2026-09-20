import { readdir, readFile, stat } from "node:fs/promises";
import { join } from "node:path";
import { fileURLToPath } from "node:url";

const distDir = new URL("../dist/", import.meta.url);
const assetsDir = new URL("../dist/assets/", import.meta.url);
const assetsPath = fileURLToPath(assetsDir);
const maxBytes = 500_000;
const files = (await readdir(assetsDir)).filter((name) => name.endsWith(".js"));

if (files.length === 0) {
	throw new Error(`No JavaScript chunks found in ${assetsDir.pathname}`);
}

const chunks = await Promise.all(
	files.map(async (name) => ({
		name,
		size: (await stat(join(assetsPath, name))).size,
	})),
);
chunks.sort((left, right) => right.size - left.size);

const oversized = chunks.filter(({ size }) => size > maxBytes);
if (oversized.length > 0) {
	for (const { name, size } of oversized) {
		console.error(`${name}: ${size} bytes exceeds ${maxBytes}-byte budget`);
	}
	process.exitCode = 1;
} else {
	const largest = chunks[0];
	console.log(
		`Bundle budget passed: largest chunk ${largest.name} is ${largest.size} bytes`,
	);
}

// Post-build content contract (issue #496): the production dist that gets
// go:embed'd into the binary must keep the static login shell (first paint
// before the React bundle executes) and must NEVER ship the test-only MSW
// service worker.
const indexHtml = await readFile(new URL("index.html", distDir), "utf8");
for (const marker of ['id="login-username"', 'id="login-password"']) {
	if (!indexHtml.includes(marker)) {
		throw new Error(
			`dist/index.html lost the static login shell marker ${marker}`,
		);
	}
}

async function walk(dir) {
	const entries = await readdir(dir, { withFileTypes: true });
	const paths = [];
	for (const entry of entries) {
		const full = join(dir, entry.name);
		if (entry.isDirectory()) paths.push(...(await walk(full)));
		else paths.push(full);
	}
	return paths;
}
const distFiles = await walk(fileURLToPath(distDir));
const shippedWorker = distFiles.find((p) => /mockServiceWorker\.js$/.test(p));
if (shippedWorker) {
	throw new Error(
		`mockServiceWorker.js must not ship in production dist: ${shippedWorker}`,
	);
}
console.log("dist content contract passed: login shell kept, MSW excluded");
