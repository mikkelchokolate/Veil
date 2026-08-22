import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";

describe("mock service worker security", () => {
	it("accepts messages only from the panel origin", () => {
		const worker = readFileSync(
			resolve(process.cwd(), "public/mockServiceWorker.js"),
			"utf8",
		);

		expect(worker).toContain("event.origin !== self.location.origin");
		expect(worker).toContain("return");
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
