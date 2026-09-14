import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { AuthProvider } from "../auth/AuthContext";
import { I18nProvider } from "../i18n/I18nContext";
import { InboundsPage } from "../pages/InboundsPage";
import { HttpResponse, http, server } from "./server";

function renderInbounds() {
	const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
	return render(
		<QueryClientProvider client={qc}>
			<AuthProvider>
				<I18nProvider>
					<InboundsPage />
				</I18nProvider>
			</AuthProvider>
		</QueryClientProvider>,
	);
}

const naiveCatalog = [
	{
		protocol: "hysteria2",
		displayName: "Hysteria2",
		transports: ["udp"],
		inboundFieldSchema: [
			{
				key: "masqueradeURL",
				label: "Masquerade URL",
				type: "text",
				default: "https://www.bing.com/",
			},
		],
	},
	{
		protocol: "naiveproxy",
		displayName: "NaiveProxy",
		transports: ["tcp"],
		inboundFieldSchema: [
			{
				key: "publicPort",
				label: "Public port",
				type: "number",
				default: 443,
			},
			{
				key: "naiveUsername",
				label: "Naive Username",
				type: "text",
				default: "veil",
			},
			{
				key: "fallbackRoot",
				label: "Fallback Root",
				type: "text",
				default: "/var/lib/veil/www",
			},
		],
	},
];

describe("InboundsPage create payload", () => {
	it("writes schema defaults into protocolFields and the dual-copy flat keys", async () => {
		const posts: Array<Record<string, unknown>> = [];
		server.use(
			http.get("/api/inbounds", () => HttpResponse.json([])),
			http.get("/api/v1/clients", () =>
				HttpResponse.json({ items: [], total: 0, page: 1, pageSize: 500 }),
			),
			http.get("/api/inbounds/:name/clients", () =>
				HttpResponse.json({ items: [], total: 0, page: 1, pageSize: 500 }),
			),
			http.get("/api/protocols", () =>
				HttpResponse.json([
					{
						protocol: "hysteria2",
						displayName: "Hysteria2",
						transports: ["udp"],
						inboundFieldSchema: [
							{
								key: "masqueradeURL",
								label: "Masquerade URL",
								type: "text",
								default: "https://www.bing.com/",
							},
						],
					},
				]),
			),
			http.post("/api/inbounds", async ({ request }) => {
				posts.push((await request.json()) as Record<string, unknown>);
				return HttpResponse.json({ name: "edge", success: true });
			}),
		);
		renderInbounds();
		await screen.findByText(/no inbounds configured/i);
		fireEvent.click(screen.getByRole("button", { name: /new inbound/i }));
		fireEvent.change(await screen.findByLabelText(/^name$/i), {
			target: { value: "edge" },
		});
		fireEvent.click(screen.getByRole("button", { name: /^create$/i }));
		await waitFor(() => expect(posts).toHaveLength(1));
		const body = posts[0];
		expect(body).toBeDefined();
		expect(body?.masqueradeURL).toBe("https://www.bing.com/");
		expect(
			(body?.protocolFields as Record<string, unknown> | undefined)
				?.masqueradeURL,
		).toBe("https://www.bing.com/");
	});

	it("does not inject schema defaults over live flat-only values on Edit+Save", async () => {
		const puts: Array<Record<string, unknown>> = [];
		server.use(
			http.get("/api/inbounds", () =>
				HttpResponse.json([
					{
						name: "edge",
						protocol: "naiveproxy",
						transport: "tcp",
						port: 20001,
						enabled: true,
						naiveUsername: "u1",
						fallbackRoot: "/srv/custom",
						protocolFields: {},
					},
				]),
			),
			http.get("/api/protocols", () => HttpResponse.json(naiveCatalog)),
			http.put("/api/inbounds/:name", async ({ request }) => {
				puts.push((await request.json()) as Record<string, unknown>);
				return HttpResponse.json({ name: "edge", success: true });
			}),
		);
		renderInbounds();
		fireEvent.click(await screen.findByRole("button", { name: /^edit$/i }));
		fireEvent.click(await screen.findByRole("button", { name: /^save$/i }));
		await waitFor(() => expect(puts).toHaveLength(1));
		const body = puts[0];
		const fields = body?.protocolFields as Record<string, unknown> | undefined;
		expect(body?.port).toBe(20001);
		expect(body?.naiveUsername).toBe("u1");
		expect(body?.fallbackRoot).toBe("/srv/custom");
		expect(
			fields?.publicPort === undefined || fields?.publicPort === 20001,
		).toBe(true);
		expect(fields?.publicPort).not.toBe(443);
		expect(
			fields?.naiveUsername === undefined || fields?.naiveUsername === "u1",
		).toBe(true);
		expect(fields?.naiveUsername).not.toBe("veil");
		expect(
			fields?.fallbackRoot === undefined ||
				fields?.fallbackRoot === "/srv/custom",
		).toBe(true);
	});

	it("does not overwrite a live Hysteria2 masquerade with the schema default", async () => {
		const puts: Array<Record<string, unknown>> = [];
		server.use(
			http.get("/api/inbounds", () =>
				HttpResponse.json([
					{
						name: "hy",
						protocol: "hysteria2",
						transport: "udp",
						port: 443,
						enabled: true,
						masqueradeURL: "https://live.example",
						protocolFields: {},
					},
				]),
			),
			http.get("/api/protocols", () =>
				HttpResponse.json([
					{
						protocol: "hysteria2",
						displayName: "Hysteria2",
						transports: ["udp"],
						inboundFieldSchema: [
							{
								key: "masqueradeURL",
								label: "Masquerade URL",
								type: "text",
								default: "https://example.com",
							},
						],
					},
				]),
			),
			http.put("/api/inbounds/:name", async ({ request }) => {
				puts.push((await request.json()) as Record<string, unknown>);
				return HttpResponse.json({ name: "hy", success: true });
			}),
		);
		renderInbounds();
		fireEvent.click(await screen.findByRole("button", { name: /^edit$/i }));
		fireEvent.click(await screen.findByRole("button", { name: /^save$/i }));
		await waitFor(() => expect(puts).toHaveLength(1));
		const body = puts[0];
		const fields = body?.protocolFields as Record<string, unknown> | undefined;
		expect(body?.masqueradeURL).toBe("https://live.example");
		expect(
			fields?.masqueradeURL === undefined ||
				fields?.masqueradeURL === "https://live.example",
		).toBe(true);
		expect(fields?.masqueradeURL).not.toBe("https://example.com");
	});

	it("sends NaiveProxy publicPort from Port instead of schema 443", async () => {
		const posts: Array<Record<string, unknown>> = [];
		server.use(
			http.get("/api/inbounds", () => HttpResponse.json([])),
			http.get("/api/protocols", () => HttpResponse.json(naiveCatalog)),
			http.post("/api/inbounds", async ({ request }) => {
				posts.push((await request.json()) as Record<string, unknown>);
				return HttpResponse.json({ name: "edge", success: true });
			}),
		);
		renderInbounds();
		await screen.findByText(/no inbounds configured/i);
		fireEvent.click(screen.getByRole("button", { name: /new inbound/i }));
		fireEvent.change(await screen.findByLabelText(/^name$/i), {
			target: { value: "edge" },
		});
		fireEvent.change(screen.getByLabelText(/^protocol$/i), {
			target: { value: "naiveproxy" },
		});
		fireEvent.change(screen.getByLabelText(/^port$/i), {
			target: { value: "20001" },
		});
		fireEvent.click(screen.getByRole("button", { name: /^create$/i }));
		await waitFor(() => expect(posts).toHaveLength(1));
		const body = posts[0];
		const fields = body?.protocolFields as Record<string, unknown> | undefined;
		expect(body?.port).toBe(20001);
		expect(
			fields?.publicPort === undefined || fields?.publicPort === 20001,
		).toBe(true);
		expect(fields?.publicPort).not.toBe(443);
	});

	it("prefills new NaiveProxy inbounds from settings.defaultInboundPublicPort", async () => {
		const posts: Array<Record<string, unknown>> = [];
		server.use(
			http.get("/api/settings", () =>
				HttpResponse.json({
					mode: "prod",
					panelListen: "127.0.0.1:2096",
					defaultInboundPublicPort: 8443,
				}),
			),
			http.get("/api/inbounds", () => HttpResponse.json([])),
			http.get("/api/protocols", () => HttpResponse.json(naiveCatalog)),
			http.post("/api/inbounds", async ({ request }) => {
				posts.push((await request.json()) as Record<string, unknown>);
				return HttpResponse.json({ name: "edge", success: true });
			}),
		);
		renderInbounds();
		await screen.findByText(/no inbounds configured/i);
		fireEvent.click(screen.getByRole("button", { name: /new inbound/i }));
		fireEvent.change(await screen.findByLabelText(/^name$/i), {
			target: { value: "edge" },
		});
		fireEvent.change(screen.getByLabelText(/^protocol$/i), {
			target: { value: "naiveproxy" },
		});
		fireEvent.click(screen.getByRole("button", { name: /^create$/i }));
		await waitFor(() => expect(posts).toHaveLength(1));
		const body = posts[0];
		const fields = body?.protocolFields as Record<string, unknown> | undefined;
		expect(body?.port).toBe(8443);
		expect(
			fields?.publicPort === undefined || fields?.publicPort === 8443,
		).toBe(true);
		expect(fields?.publicPort).not.toBe(443);
	});

	it("does not treat a failed attached-clients fetch as none", async () => {
		server.use(
			http.get("/api/inbounds", () =>
				HttpResponse.json([
					{
						name: "edge",
						protocol: "hysteria2",
						transport: "udp",
						port: 443,
						enabled: true,
					},
				]),
			),
			http.get("/api/protocols", () =>
				HttpResponse.json([
					{
						protocol: "hysteria2",
						displayName: "Hysteria2",
						transports: ["udp"],
					},
				]),
			),
			http.get("/api/inbounds/:name/clients", () =>
				HttpResponse.json(
					{ error: { message: "clients down" } },
					{ status: 500 },
				),
			),
		);
		renderInbounds();
		expect(await screen.findByText(/clients down/i)).toBeInTheDocument();
	});
});
