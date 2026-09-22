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

describe("InboundsPage effective port (#717)", () => {
	// A schema inbound whose port lives in protocolFields.publicPort — the
	// flat `port` field is absent, so `ib.port ?? "—"` rendered a dash and the
	// row toggle dropped the port entirely.
	it("renders protocolFields.publicPort and carries it through the row toggle", async () => {
		const puts: Array<Record<string, unknown>> = [];
		server.use(
			http.get("/api/inbounds", () =>
				HttpResponse.json([
					{
						name: "edge",
						protocol: "naiveproxy",
						transport: "tcp",
						enabled: true,
						protocolFields: { publicPort: 8443 },
					},
				]),
			),
			http.get("/api/protocols", () =>
				HttpResponse.json([
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
						],
					},
				]),
			),
			http.put("/api/inbounds/edge", async ({ request }) => {
				puts.push((await request.json()) as Record<string, unknown>);
				return HttpResponse.json({ success: true });
			}),
		);
		renderInbounds();
		const row = (await screen.findByText("edge")).closest("tr");
		expect(row).not.toBeNull();
		const cells = Array.from(row?.querySelectorAll("td") ?? []);
		expect(cells.some((td) => td.textContent?.trim() === "8443")).toBe(true);

		fireEvent.click(await screen.findByRole("button", { name: /^disable$/i }));
		await waitFor(() => expect(puts).toHaveLength(1));
		expect(puts[0]?.port).toBe(8443);
		expect(
			(puts[0]?.protocolFields as Record<string, unknown> | undefined)
				?.publicPort,
		).toBe(8443);
		expect(puts[0]?.enabled).toBe(false);
	});
});
