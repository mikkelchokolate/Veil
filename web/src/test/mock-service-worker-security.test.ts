import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import vm from "node:vm";
import { describe, expect, it, vi } from "vitest";

interface WorkerHarness {
	message: (event: {
		origin: string;
		source?: { id?: string };
		data?: unknown;
	}) => Promise<void>;
	clientsGet: ReturnType<typeof vi.fn>;
}

// Load the real worker source in a vm sandbox so the origin guard is verified
// behaviorally — a foreign-origin message must return before touching
// self.clients, while a same-origin one reaches it (issue #490).
function loadWorker(origin = "https://panel.example"): WorkerHarness {
	const worker = readFileSync(
		resolve(process.cwd(), "public/mockServiceWorker.js"),
		"utf8",
	);
	const handlers = new Map<string, (event: unknown) => unknown>();
	const clientsGet = vi.fn(async () => ({
		id: "client-1",
		frameType: "nested",
		postMessage: vi.fn(),
	}));
	const sandbox = {
		self: {
			location: { origin },
			clients: {
				get: clientsGet,
				matchAll: async () => [],
				claim: async () => undefined,
			},
			registration: { unregister: vi.fn(async () => true) },
			skipWaiting: vi.fn(async () => undefined),
		},
		addEventListener: (type: string, handler: (event: unknown) => unknown) => {
			handlers.set(type, handler);
		},
		// sendToClient constructs a channel per reply; keep it functional so a
		// same-origin message completes its path.
		MessageChannel,
	};
	vm.createContext(sandbox);
	vm.runInContext(worker, sandbox);
	const message = handlers.get("message");
	if (!message) throw new Error("worker did not register a message handler");
	return {
		message: message as WorkerHarness["message"],
		clientsGet,
	};
}

describe("mock service worker security", () => {
	it("rejects control messages from a foreign origin before touching clients", async () => {
		const { message, clientsGet } = loadWorker();
		await message({
			origin: "https://evil.example",
			source: { id: "client-1" },
			data: "MOCK_ACTIVATE",
		});
		expect(clientsGet).not.toHaveBeenCalled();
	});

	it("still processes control messages from the panel origin", async () => {
		const { message, clientsGet } = loadWorker();
		await message({
			origin: "https://panel.example",
			source: { id: "client-1" },
			data: "INTEGRITY_CHECK_REQUEST",
		});
		expect(clientsGet).toHaveBeenCalledWith("client-1");
	});

	it("restores the origin guard after dependency installation", () => {
		const packageJSON = JSON.parse(
			readFileSync(resolve(process.cwd(), "package.json"), "utf8"),
		) as { scripts?: Record<string, string> };
		const preparationScript = readFileSync(
			resolve(process.cwd(), "scripts/prepare_msw_worker.mjs"),
			"utf8",
		);

		expect(packageJSON.scripts?.postinstall).toBe(
			"node scripts/prepare_msw_worker.mjs",
		);
		expect(preparationScript).toContain(
			"event.origin !== self.location.origin",
		);
		expect(preparationScript).toContain("MSW worker message handler changed");
	});

	it("provides postinstall inputs before the container install layer", () => {
		const dockerfile = readFileSync(
			resolve(process.cwd(), "../Dockerfile"),
			"utf8",
		);
		const install = dockerfile.indexOf("RUN pnpm install --frozen-lockfile");
		const scriptCopy = dockerfile.indexOf(
			"COPY web/scripts/prepare_msw_worker.mjs ./scripts/",
		);
		const workerCopy = dockerfile.indexOf(
			"COPY web/public/mockServiceWorker.js ./public/",
		);

		expect(install).toBeGreaterThan(-1);
		expect(scriptCopy).toBeGreaterThan(-1);
		expect(workerCopy).toBeGreaterThan(-1);
		expect(scriptCopy).toBeLessThan(install);
		expect(workerCopy).toBeLessThan(install);
	});
});
