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

const catalog = [
	{
		protocol: "hysteria2",
		displayName: "Hysteria2",
		transports: ["udp"],
		inboundFieldSchema: [],
	},
];

const inbound = {
	name: "edge",
	protocol: "hysteria2",
	transport: "udp",
	port: 443,
	enabled: true,
	protocolFields: {},
};

// #709: row enable/disable fires a full PUT that drops live access for every
// attached client — it must confirm before sending, like row Delete.
describe("InboundsPage row enable/disable confirm", () => {
	it("does not PUT until disable is confirmed", async () => {
		const puts: Array<Record<string, unknown>> = [];
		server.use(
			http.get("/api/inbounds", () => HttpResponse.json([inbound])),
			http.get("/api/protocols", () => HttpResponse.json(catalog)),
			http.put("/api/inbounds/edge", async ({ request }) => {
				puts.push((await request.json()) as Record<string, unknown>);
				return HttpResponse.json({ ...inbound, enabled: false });
			}),
		);
		renderInbounds();
		fireEvent.click(await screen.findByRole("button", { name: /^disable$/i }));
		expect(await screen.findByRole("alertdialog")).toBeInTheDocument();
		expect(puts).toHaveLength(0);
		fireEvent.click(screen.getByRole("button", { name: /confirm disable/i }));
		await waitFor(() => expect(puts).toHaveLength(1));
		expect(puts[0]).toMatchObject({ name: "edge", enabled: false });
	});

	it("does not PUT until enable is confirmed", async () => {
		const puts: Array<Record<string, unknown>> = [];
		server.use(
			http.get("/api/inbounds", () =>
				HttpResponse.json([{ ...inbound, enabled: false }]),
			),
			http.get("/api/protocols", () => HttpResponse.json(catalog)),
			http.put("/api/inbounds/edge", async ({ request }) => {
				puts.push((await request.json()) as Record<string, unknown>);
				return HttpResponse.json(inbound);
			}),
		);
		renderInbounds();
		fireEvent.click(await screen.findByRole("button", { name: /^enable$/i }));
		expect(await screen.findByRole("alertdialog")).toBeInTheDocument();
		expect(puts).toHaveLength(0);
		fireEvent.click(screen.getByRole("button", { name: /confirm enable/i }));
		await waitFor(() => expect(puts).toHaveLength(1));
		expect(puts[0]).toMatchObject({ name: "edge", enabled: true });
	});
});
