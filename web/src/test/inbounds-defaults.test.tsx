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

describe("InboundsPage create payload", () => {
	it("writes schema defaults into protocolFields and the dual-copy flat keys", async () => {
		const posts: Array<Record<string, unknown>> = [];
		server.use(
			http.get("/api/inbounds", () => HttpResponse.json([])),
			http.get("/api/v1/clients", () =>
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
			http.get("/api/v1/clients", () =>
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
