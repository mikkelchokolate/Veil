import { http, HttpResponse } from "msw";
import { setupServer } from "msw/node";

// Default handlers for the Veil management API used by the SPA tests.
// Individual tests override these with server.use(...) for their scenario.
export const server = setupServer(
	http.get("/api/setup/status", () =>
		HttpResponse.json({ required: false, allowed: false, completed: true }),
	),
	http.get("/api/auth/status", () =>
		HttpResponse.json({
			authenticated: true,
			username: "admin",
			role: "admin",
			csrfToken: "test-csrf",
		}),
	),
	http.get("/api/apply/state", () =>
		HttpResponse.json({
			desiredRevision: 1,
			appliedRevision: 1,
			state: "applied",
		}),
	),
);

export { http, HttpResponse };
