import { readFile, writeFile } from "node:fs/promises";
import { resolve } from "node:path";

const workerPath = resolve(process.cwd(), "public/mockServiceWorker.js");
const messageHandler = "addEventListener('message', async function (event) {\n";
const originGuard =
	"  if (event.origin !== self.location.origin) {\n    return\n  }\n";

const worker = await readFile(workerPath, "utf8");
if (worker.includes(originGuard)) {
	process.exit(0);
}
if (!worker.includes(messageHandler)) {
	throw new Error(
		"MSW worker message handler changed; update the origin guard insertion point",
	);
}
const prepared = worker.replace(
	messageHandler,
	`${messageHandler}  // Accept control messages only from clients served by this panel origin.\n${originGuard}\n`,
);
await writeFile(workerPath, prepared);
