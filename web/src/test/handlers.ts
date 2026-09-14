import { HttpResponse, http } from "msw";

// Isomorphic default handlers for the Veil management API, shared by the node
// (jsdom) server and the real-browser worker (blocker W8). This module must
// stay environment-neutral: import from "msw" only, never "msw/node" or
// "msw/browser". Individual tests override these with server.use(...) /
// worker.use(...) for their scenario.
export const defaultHandlers = [
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
	http.get("/api/settings", () =>
		HttpResponse.json({
			mode: "prod",
			panelListen: "127.0.0.1:2096",
			defaultInboundPublicPort: 0,
		}),
	),
	http.get("/api/inbounds/:name/clients", () =>
		HttpResponse.json({ items: [], total: 0, page: 1, pageSize: 500 }),
	),
];

export { HttpResponse, http };
