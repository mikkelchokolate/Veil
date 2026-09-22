import { describe, expect, it } from "vitest";
import { escapeHtml } from "../lib/escapeHtml";

describe("escapeHtml", () => {
	it("escapes HTML metacharacters", () => {
		expect(escapeHtml(`<img src=x onerror=alert(1)>"'`)).toBe(
			"&lt;img src=x onerror=alert(1)&gt;&quot;&#39;",
		);
	});

	it("coerces non-string input instead of throwing", () => {
		expect(escapeHtml(0)).toBe("0");
		expect(escapeHtml(null)).toBe("null");
		expect(escapeHtml(undefined)).toBe("undefined");
	});
});
