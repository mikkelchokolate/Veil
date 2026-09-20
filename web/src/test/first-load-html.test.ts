import { readFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

const html = readFileSync(
	join(dirname(fileURLToPath(import.meta.url)), "../../index.html"),
	"utf8",
);

// Source-template contract only: this asserts on the Vite INPUT template. The
// shipped/production document is verified against web/dist/index.html by
// scripts/check_bundle_size.mjs at the end of every `pnpm build` — including
// the hashed-asset existence checks (#468). Nothing in the jsdom suite may
// claim production coverage: `pnpm test` runs before the build.
describe("first-load HTML template (source)", () => {
	it("declares lang, title, viewport, description, and the login shell", () => {
		expect(html).toContain('<html lang="en">');
		expect(html).toContain("<title>Veil</title>");
		expect(html).toContain('name="viewport"');
		expect(html).toContain('name="description"');
		expect(html).toContain('<main class="center-screen">');
		expect(html).toContain('id="login-username"');
		expect(html).toContain('id="login-password"');
		expect(html).toContain('rel="icon"');
		expect(html).not.toMatch(/noindex/i);
	});
});
