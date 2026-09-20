import { readFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";
import { screen, waitFor } from "@testing-library/react";
import { HttpResponse, http, server } from "./server";

// Integration coverage for the deferred-boot gate (issue #484): the static
// login shell painted by index.html stays static until page intent, then
// boot.ts dynamically imports main.tsx, which mounts the App over the shell.
// first-load.test.tsx covers the SPA layer directly; this test drives the
// handoff between them.

const indexHtml = readFileSync(
	join(dirname(fileURLToPath(import.meta.url)), "../../index.html"),
	"utf8",
);

function mountStaticShell(): void {
	const bodyMatch = indexHtml.match(/<body[^>]*>([\s\S]*)<\/body>/);
	if (!bodyMatch) throw new Error("index.html has no <body> content");
	// innerHTML-inserted <script> elements never execute — the module under
	// test is imported explicitly below, exactly like the production entry.
	document.body.innerHTML = bodyMatch[1];
}

describe("static shell → boot.ts → SPA handoff", () => {
	beforeEach(() => {
		// boot.ts keeps module-level `started` state and registers window
		// globals once; each scenario needs a fresh module instance.
		vi.resetModules();
	});
	afterEach(() => {
		document.body.innerHTML = "";
		delete window.__VEIL_BOOT;
		delete window.__VEIL_READY;
		delete window.__VEIL_PENDING_LOGIN;
	});

	it("keeps the static shell until interaction, then boots and mounts the App", async () => {
		server.use(
			http.get("/api/setup/status", () =>
				HttpResponse.json({ required: false, allowed: false, completed: true }),
			),
			http.get("/api/auth/status", () =>
				HttpResponse.json({ authenticated: false }),
			),
		);
		mountStaticShell();
		expect(screen.getByLabelText("Username")).toBeInTheDocument();
		expect(window.__VEIL_READY).toBeUndefined();

		await import("../boot");

		// Deferred boot: importing the entry must not replace the shell on its
		// own — only page intent (or a live session marker) triggers it.
		expect(window.__VEIL_BOOT).toBeTypeOf("function");
		expect(window.__VEIL_READY).toBeUndefined();
		expect(document.querySelector(".auth-card")).not.toBeNull();

		// A pointerdown outside the login card is page intent.
		document.body.dispatchEvent(new Event("pointerdown", { bubbles: true }));

		await waitFor(() => expect(window.__VEIL_READY).toBe(true));
		// The SPA mounted over the shell: same login fields, now React-owned
		// (the static-shell HTML comment inside #root is gone).
		expect(await screen.findByLabelText("Username")).toBeInTheDocument();
		const root = document.getElementById("root");
		expect(root?.innerHTML).not.toContain("Static login shell");
	});

	it("captures login fields into the pending-login handoff on submit", async () => {
		let loginBody: unknown;
		server.use(
			http.get("/api/setup/status", () =>
				HttpResponse.json({ required: false, allowed: false, completed: true }),
			),
			http.get("/api/auth/status", () =>
				HttpResponse.json({ authenticated: false }),
			),
			http.post("/api/auth/login", async ({ request }) => {
				loginBody = await request.json();
				return HttpResponse.json({ authenticated: false }, { status: 401 });
			}),
		);
		mountStaticShell();
		await import("../boot");

		const username = document.getElementById("login-username");
		const password = document.getElementById("login-password");
		if (
			!(username instanceof HTMLInputElement) ||
			!(password instanceof HTMLInputElement)
		) {
			throw new Error("static shell inputs missing");
		}
		username.value = "admin";
		password.value = "s3cret";

		const form = document.querySelector(".auth-card");
		if (!(form instanceof HTMLFormElement))
			throw new Error("static shell form missing");
		form.dispatchEvent(
			new Event("submit", { bubbles: true, cancelable: true }),
		);

		// The submit handler prevents default and boots; typed credentials are
		// handed to the SPA via __VEIL_PENDING_LOGIN, not lost with the shell.
		expect(window.__VEIL_PENDING_LOGIN).toEqual({
			username: "admin",
			password: "s3cret",
			submit: true,
		});
		await waitFor(() => expect(window.__VEIL_READY).toBe(true));
		// The SPA consumed the pending-login handoff and submitted it — the
		// typed credentials survived the static-shell teardown.
		await waitFor(() =>
			expect(loginBody).toEqual({ username: "admin", password: "s3cret" }),
		);
	});
});
