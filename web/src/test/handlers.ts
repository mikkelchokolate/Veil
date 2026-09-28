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
			state: "synced",
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
	// The client pages read the protocol catalog for quota-enforcement
	// verdicts; an empty catalog keeps the verdict undecidable (never
	// "unsupported") so tests that don't care stay neutral.
	http.get("/api/protocols", () => HttpResponse.json([])),
	http.get("/api/processes", () => HttpResponse.json({ processes: [] })),
	// Traffic page live feeds — empty defaults keep tests that render the page
	// (or the whole router) quiet; scenarios override with server.use(...).
	http.get("/api/v1/presence", () =>
		HttpResponse.json({ items: [], count: 0 }),
	),
	http.get("/api/connections", () => HttpResponse.json({ listeners: [] })),
	http.get("/api/v1/traffic/history", () =>
		HttpResponse.json({ items: null, count: 0 }),
	),
];

export { HttpResponse, http };
