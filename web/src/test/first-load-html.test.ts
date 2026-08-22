import { readFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

const html = readFileSync(
	join(dirname(fileURLToPath(import.meta.url)), "../../index.html"),
	"utf8",
);

describe("shipped first-load document", () => {
	it("declares lang, title, viewport, description, and the login shell", () => {
		expect(html).toContain('<html lang="en">');
		expect(html).toContain("<title>Veil</title>");
		expect(html).toContain('name="viewport"');
		expect(html).toContain('name="description"');
		expect(html).toContain('<main class="center-screen">');
		expect(html).toContain('id="login-username"');
		expect(html).toContain('id="login-password"');
		expect(html).toContain('rel="icon"');
		expect(html).toContain('src="./src/boot.ts"');
		expect(html).not.toMatch(/noindex/i);
	});
});
