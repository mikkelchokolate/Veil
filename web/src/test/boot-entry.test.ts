import { readFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

const dir = dirname(fileURLToPath(import.meta.url));
const boot = readFileSync(join(dir, "../boot.ts"), "utf8");
const html = readFileSync(join(dir, "../../index.html"), "utf8");

describe("first-load boot entry", () => {
	it("is the HTML module entry and only dynamically imports main", () => {
		expect(html).toContain('src="./src/boot.ts"');
		expect(html).not.toContain('src="./src/main.tsx"');
		expect(boot).toMatch(/import\("\.\/main"\)/);
		expect(boot).toContain("__VEIL_BOOT");
		expect(boot).toContain("veil_spa");
		expect(boot).toContain("captureLoginFields");
		expect(boot).not.toMatch(/addEventListener\("focusin"/);
		expect(boot).not.toMatch(/addEventListener\("keydown"/);
		expect(boot).not.toContain("/api/auth/status");
		expect(boot.indexOf("./styles.css")).toBeGreaterThan(-1);
		expect(boot.indexOf("./styles.css")).toBeLessThan(
			boot.indexOf("./legacy-theme.css"),
		);
		expect(boot).not.toMatch(/from ["']\.\/main/);
	});
});
