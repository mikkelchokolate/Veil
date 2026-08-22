import { readdir, stat } from "node:fs/promises";
import { join } from "node:path";
import { fileURLToPath } from "node:url";

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
