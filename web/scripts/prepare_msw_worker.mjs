import { readFile, writeFile } from "node:fs/promises";
import { resolve } from "node:path";

const workerPath = resolve(process.cwd(), "public/mockServiceWorker.js");
const worker = await readFile(workerPath, "utf8");
const newline = worker.includes("\r\n") ? "\r\n" : "\n";
const messageHandler = `addEventListener('message', async function (event) {${newline}`;
const originGuard = [
	"  if (event.origin !== self.location.origin) {",
	"    return",
	"  }",
	"",
].join(newline);

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
