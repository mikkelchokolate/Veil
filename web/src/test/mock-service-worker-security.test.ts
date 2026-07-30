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
	});
});
