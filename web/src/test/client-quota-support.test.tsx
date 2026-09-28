import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
	createMemoryHistory,
	createRouter,
	RouterProvider,
} from "@tanstack/react-router";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { AuthProvider } from "../auth/AuthContext";
import { I18nProvider } from "../i18n/I18nContext";
import { routeTree } from "../routeTree.gen";
import { HttpResponse, http, server } from "./server";

// Quota support must be read off the binding's advertised capability
// (quotaEnforcement), with the inbound catalog's protocol as the fallback
// only when a binding carries none. The UI warns while the operator is
// still editing instead of failing only on submit; the submit path keeps a
// backstop and the backend re-validates independently.

function renderClientDetail() {
	const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
	const router = createRouter({
		routeTree,
		history: createMemoryHistory({ initialEntries: ["/clients/c1"] }),
	});
	return render(
		<QueryClientProvider client={qc}>
			<AuthProvider>
				<I18nProvider>
					<RouterProvider router={router} />
				</I18nProvider>
			</AuthProvider>
		</QueryClientProvider>,
	);
}

function renderNewClient() {
	const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
	const router = createRouter({
		routeTree,
		history: createMemoryHistory({ initialEntries: ["/clients/new"] }),
	});
	return render(
		<QueryClientProvider client={qc}>
			<AuthProvider>
				<I18nProvider>
					<RouterProvider router={router} />
				</I18nProvider>
			</AuthProvider>
		</QueryClientProvider>,
	);
}

function mieruCapability() {
	return {
		protocol: "mieru",
		transports: ["tcp"],
		perClientCredentials: true,
		requiresCaddy: false,
		trafficAccounting: true,
		quotaEnforcement: false,
		expirationEnforcement: true,
	};
}

// The /api/protocols catalog carries the same verdict as the binding
// capability surface — the fallback signal for drafts and capability-less
// payloads.
function protocolsCatalogMock() {
	return http.get("/api/protocols", () =>
		HttpResponse.json([
			{
				protocol: "hysteria2",
				displayName: "Hysteria2",
				transports: ["udp"],
				trafficAccounting: true,
				quotaEnforcement: true,
			},
			{
				protocol: "mieru",
				displayName: "Mieru",
				transports: ["tcp"],
				trafficAccounting: true,
				quotaEnforcement: false,
			},
		]),
	);
}

describe("ClientDetailPage quota capability hints", () => {
	it("warns inline while filling quota and blocks save when an enabled binding can't enforce it", async () => {
		const patches: Array<Record<string, unknown>> = [];
		server.use(
			http.get("/api/v1/clients/c1", () =>
				HttpResponse.json({
					id: "c1",
					name: "Alice",
					enabled: true,
					version: 1,
					bindings: [
						{
							id: "b1",
							inboundId: "edge",
							enabled: true,
							capability: mieruCapability(),
						},
					],
				}),
			),
			http.get("/api/inbounds", () =>
				HttpResponse.json([{ name: "edge", protocol: "mieru", enabled: true }]),
			),
			http.patch("/api/v1/clients/c1", async ({ request }) => {
				patches.push((await request.json()) as Record<string, unknown>);
				return HttpResponse.json({ success: true });
			}),
			protocolsCatalogMock(),
		);
		const user = userEvent.setup();
		renderClientDetail();
		const quota = await screen.findByLabelText(/quota/i);
		await user.type(quota, "500");
		// The hint names the blocking binding while the user is still filling
		// the field — before any submit attempt.
		expect(
			await screen.findByText(/can't be enforced on edge \(mieru\)/i),
		).toBeInTheDocument();
		await user.click(screen.getByRole("button", { name: /save changes/i }));
		expect(
			await screen.findAllByText(/can't be enforced on edge \(mieru\)/i),
		).not.toHaveLength(0);
		expect(patches).toHaveLength(0);
	});

	it("marks the binding card as not enforcing quota from the advertised capability", async () => {
		server.use(
			http.get("/api/v1/clients/c1", () =>
				HttpResponse.json({
					id: "c1",
					name: "Alice",
					enabled: true,
					version: 1,
					bindings: [
						{
							id: "b1",
							inboundId: "edge",
							enabled: true,
							capability: mieruCapability(),
						},
					],
				}),
			),
			http.get("/api/inbounds", () =>
				HttpResponse.json([{ name: "edge", protocol: "mieru", enabled: true }]),
			),
		);
		const user = userEvent.setup();
		renderClientDetail();
		await user.click(
			await screen.findByRole("tab", { name: /^access$/i }, { timeout: 5000 }),
		);
		expect(await screen.findByText(/quota not enforced/i)).toBeInTheDocument();
	});

	it("falls back to the /api/protocols verdict when the binding carries no capability", async () => {
		const patches: Array<Record<string, unknown>> = [];
		server.use(
			http.get("/api/v1/clients/c1", () =>
				HttpResponse.json({
					id: "c1",
					name: "Alice",
					enabled: true,
					version: 1,
					// No capability object — the /api/protocols catalog is the
					// fallback signal, exactly like a pre-enrichment payload.
					bindings: [{ id: "b1", inboundId: "edge", enabled: true }],
				}),
			),
			http.get("/api/inbounds", () =>
				HttpResponse.json([{ name: "edge", protocol: "mieru", enabled: true }]),
			),
			http.patch("/api/v1/clients/c1", async ({ request }) => {
				patches.push((await request.json()) as Record<string, unknown>);
				return HttpResponse.json({ success: true });
			}),
			protocolsCatalogMock(),
		);
		const user = userEvent.setup();
		renderClientDetail();
		const quota = await screen.findByLabelText(/quota/i);
		await user.type(quota, "500");
		expect(
			await screen.findByText(/can't be enforced on edge \(mieru\)/i),
		).toBeInTheDocument();
		await user.click(screen.getByRole("button", { name: /save changes/i }));
		expect(
			await screen.findAllByText(/can't be enforced on edge \(mieru\)/i),
		).not.toHaveLength(0);
		expect(patches).toHaveLength(0);
	});

	it("saves a quota on a hysteria2 binding without showing the unsupported hint", async () => {
		const patches: Array<Record<string, unknown>> = [];
		server.use(
			http.get("/api/v1/clients/c1", () =>
				HttpResponse.json({
					id: "c1",
					name: "Alice",
					enabled: true,
					version: 1,
					bindings: [
						{
							id: "b1",
							inboundId: "edge",
							enabled: true,
							capability: {
								protocol: "hysteria2",
								transports: ["udp"],
								perClientCredentials: true,
								requiresCaddy: false,
								trafficAccounting: true,
								quotaEnforcement: true,
								expirationEnforcement: true,
							},
						},
					],
				}),
			),
			http.get("/api/inbounds", () =>
				HttpResponse.json([
					{ name: "edge", protocol: "hysteria2", enabled: true },
				]),
			),
			http.patch("/api/v1/clients/c1", async ({ request }) => {
				patches.push((await request.json()) as Record<string, unknown>);
				return HttpResponse.json({ success: true });
			}),
			protocolsCatalogMock(),
		);
		const user = userEvent.setup();
		renderClientDetail();
		const quota = await screen.findByLabelText(/quota/i);
		await user.type(quota, "500");
		expect(screen.queryByText(/can't be enforced/i)).not.toBeInTheDocument();
		await user.click(screen.getByRole("button", { name: /save changes/i }));
		await waitFor(() => expect(patches).toHaveLength(1));
		expect(patches[0]?.quotaBytes).toBe(500);
	});
});

describe("ClientNewPage quota capability hints", () => {
	it("warns while picking a quota-unsupported inbound and blocks create", async () => {
		let posts = 0;
		server.use(
			http.get("/api/inbounds", () =>
				HttpResponse.json([
					{ name: "hy2", protocol: "hysteria2", enabled: true },
					{ name: "mi", protocol: "mieru", enabled: true },
				]),
			),
			protocolsCatalogMock(),
			http.post("/api/v1/clients", () => {
				posts += 1;
				return HttpResponse.json(
					{
						client: {
							id: "c1",
							name: "alice",
							enabled: true,
							version: 1,
							status: "active",
						},
						success: true,
						revision: { desired: 2, applied: 2, state: "synced" },
					},
					{ status: 201 },
				);
			}),
		);
		const user = userEvent.setup();
		renderNewClient();
		await user.type(await screen.findByLabelText(/^name$/i), "alice");
		await user.click(screen.getByRole("button", { name: /^next$/i }));
		await user.type(await screen.findByLabelText(/quota/i), "500");
		await user.click(screen.getByRole("button", { name: /^next$/i }));
		// Access step: the picked inbound is labeled honestly and the warning
		// appears as soon as the unsupported binding is selected.
		expect(await screen.findByText(/quota not enforced/i)).toBeInTheDocument();
		await user.click(await screen.findByRole("checkbox", { name: /^mi/i }));
		expect(
			await screen.findByText(/can't be enforced on mi \(mieru\)/i),
		).toBeInTheDocument();
		await user.click(screen.getByRole("button", { name: /^review$/i }));
		await user.click(screen.getByRole("button", { name: /create client/i }));
		expect(
			await screen.findAllByText(/can't be enforced on mi \(mieru\)/i),
		).not.toHaveLength(0);
		expect(posts).toBe(0);
	});
});
