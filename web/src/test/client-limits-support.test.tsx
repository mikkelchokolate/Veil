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

// Connection-limit (deviceLimit/ipLimit) support is read off the binding's
// advertised capability (deviceLimits), with the /api/protocols catalog
// verdict as the fallback for capability-less payloads and create drafts.
// Mirrors client-quota-support.test.tsx — same precedence, same honest-hint
// behavior; the server re-validates independently.

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
		deviceLimits: false,
		expirationEnforcement: true,
	};
}

function hysteria2Capability() {
	return {
		protocol: "hysteria2",
		transports: ["udp"],
		perClientCredentials: true,
		requiresCaddy: false,
		trafficAccounting: true,
		quotaEnforcement: true,
		deviceLimits: true,
		expirationEnforcement: true,
	};
}

function protocolsCatalogMock() {
	return http.get("/api/protocols", () =>
		HttpResponse.json([
			{
				protocol: "hysteria2",
				displayName: "Hysteria2",
				transports: ["udp"],
				trafficAccounting: true,
				quotaEnforcement: true,
				deviceLimits: true,
			},
			{
				protocol: "mieru",
				displayName: "Mieru",
				transports: ["tcp"],
				trafficAccounting: true,
				quotaEnforcement: false,
				deviceLimits: false,
			},
		]),
	);
}

describe("ClientDetailPage connection-limit capability hints", () => {
	it("warns inline while filling limits and blocks save when an enabled binding can't enforce them", async () => {
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
		const deviceLimit = await screen.findByLabelText(/device limit/i);
		await user.type(deviceLimit, "2");
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

	it("marks the binding card as not enforcing session limits from the advertised capability", async () => {
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
		expect(await screen.findByText(/no session limits/i)).toBeInTheDocument();
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
		const ipLimit = await screen.findByLabelText(/ip limit/i);
		await user.type(ipLimit, "1");
		expect(
			await screen.findByText(/can't be enforced on edge \(mieru\)/i),
		).toBeInTheDocument();
		await user.click(screen.getByRole("button", { name: /save changes/i }));
		expect(
			await screen.findAllByText(/can't be enforced on edge \(mieru\)/i),
		).not.toHaveLength(0);
		expect(patches).toHaveLength(0);
	});

	it("saves limits on a hysteria2 binding without showing the unsupported hint", async () => {
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
							capability: hysteria2Capability(),
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
		const deviceLimit = await screen.findByLabelText(/device limit/i);
		await user.type(deviceLimit, "3");
		const ipLimit = screen.getByLabelText(/ip limit/i);
		await user.type(ipLimit, "2");
		expect(screen.queryByText(/can't be enforced/i)).not.toBeInTheDocument();
		await user.click(screen.getByRole("button", { name: /save changes/i }));
		await waitFor(() => expect(patches).toHaveLength(1));
		expect(patches[0]?.deviceLimit).toBe(3);
		expect(patches[0]?.ipLimit).toBe(2);
	});

	it("clears a persisted limit by sending an explicit null", async () => {
		const patches: Array<Record<string, unknown>> = [];
		server.use(
			http.get("/api/v1/clients/c1", () =>
				HttpResponse.json({
					id: "c1",
					name: "Alice",
					enabled: true,
					version: 1,
					deviceLimit: 2,
					ipLimit: 4,
					bindings: [
						{
							id: "b1",
							inboundId: "edge",
							enabled: true,
							capability: hysteria2Capability(),
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
		await user.clear(await screen.findByLabelText(/device limit/i));
		await user.click(screen.getByRole("button", { name: /save changes/i }));
		await waitFor(() => expect(patches).toHaveLength(1));
		expect(patches[0]?.deviceLimit).toBeNull();
		expect(patches[0]?.ipLimit).toBeUndefined();
	});
});

describe("ClientNewPage connection-limit capability hints", () => {
	it("warns while picking a limit-unsupported inbound and blocks create", async () => {
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
		await user.type(await screen.findByLabelText(/device limit/i), "2");
		await user.click(screen.getByRole("button", { name: /^next$/i }));
		// Access step: the picked inbound is labeled honestly and the warning
		// appears as soon as the unsupported binding is selected.
		expect(await screen.findByText(/no session limits/i)).toBeInTheDocument();
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

	it("rejects non-positive limit input before it can reach the API", async () => {
		let posts = 0;
		server.use(
			http.get("/api/inbounds", () =>
				HttpResponse.json([
					{ name: "hy2", protocol: "hysteria2", enabled: true },
				]),
			),
			protocolsCatalogMock(),
			http.post("/api/v1/clients", () => {
				posts += 1;
				return HttpResponse.json({}, { status: 201 });
			}),
		);
		const user = userEvent.setup();
		renderNewClient();
		await user.type(await screen.findByLabelText(/^name$/i), "alice");
		await user.click(screen.getByRole("button", { name: /^next$/i }));
		await user.type(await screen.findByLabelText(/device limit/i), "0");
		expect(await screen.findByText(/whole number ≥ 1/i)).toBeInTheDocument();
		expect(screen.getByRole("button", { name: /^next$/i })).toBeDisabled();
		expect(posts).toBe(0);
	});
});
