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

const failedOutcome = {
	success: false,
	revision: { desired: 2, applied: 1, state: "failed" },
	applyJob: { id: "job-1", status: "failed" },
};

// #649: inbound create/update/delete can commit while the auto-apply fails
// (success=false in a 2xx). The page must keep the editor/confirm open and
// surface the failure instead of dismissing it as a clean mutation.
describe("InboundsPage apply-failed outcomes", () => {
	it("keeps the create dialog open when the inbound commits but apply fails", async () => {
		server.use(
			http.get("/api/inbounds", () => HttpResponse.json([])),
			http.get("/api/protocols", () => HttpResponse.json(catalog)),
			http.post("/api/inbounds", () =>
				HttpResponse.json({ name: "edge", ...failedOutcome }, { status: 201 }),
			),
		);
		renderInbounds();
		await screen.findByText(/no inbounds configured/i);
		fireEvent.click(screen.getByRole("button", { name: /new inbound/i }));
		fireEvent.change(await screen.findByLabelText(/^name$/i), {
			target: { value: "edge" },
		});
		fireEvent.click(screen.getByRole("button", { name: /^create$/i }));
		// The editor stays open with the failure visible in context.
		expect(await screen.findAllByText(/applying it failed/i)).not.toHaveLength(
			0,
		);
		expect(
			screen.getByRole("button", { name: /^create$/i }),
		).toBeInTheDocument();
		// The committed outcome is still recorded in the feedback card —
		// with the danger badge, not a green "saved".
		await waitFor(() =>
			expect(screen.getAllByText(/^apply failed$/i).length).toBeGreaterThan(0),
		);
	});

	it("keeps the edit dialog open when the update commits but apply fails", async () => {
		server.use(
			http.get("/api/inbounds", () =>
				HttpResponse.json([
					{
						name: "edge",
						protocol: "hysteria2",
						transport: "udp",
						port: 443,
						enabled: true,
						protocolFields: {},
					},
				]),
			),
			http.get("/api/protocols", () => HttpResponse.json(catalog)),
			http.put("/api/inbounds/edge", () =>
				HttpResponse.json({ name: "edge", ...failedOutcome }),
			),
		);
		renderInbounds();
		fireEvent.click(await screen.findByRole("button", { name: /^edit$/i }));
		fireEvent.click(await screen.findByRole("button", { name: /^save$/i }));
		expect(await screen.findAllByText(/applying it failed/i)).not.toHaveLength(
			0,
		);
		expect(screen.getByRole("button", { name: /^save$/i })).toBeInTheDocument();
	});

	it("keeps the delete confirm open when the delete commits but apply fails", async () => {
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
			http.get("/api/protocols", () => HttpResponse.json(catalog)),
			http.delete("/api/inbounds/edge", () =>
				HttpResponse.json({ name: "edge", ...failedOutcome }),
			),
		);
		renderInbounds();
		fireEvent.click(await screen.findByRole("button", { name: /^delete$/i }));
		fireEvent.click(
			await screen.findByRole("button", { name: /confirm delete/i }),
		);
		expect(await screen.findAllByText(/applying it failed/i)).not.toHaveLength(
			0,
		);
		// The confirm dialog is still open — the operator can see the
		// committed delete did not converge before deciding what to do.
		expect(
			screen.getByRole("button", { name: /confirm delete/i }),
		).toBeInTheDocument();
	});
});
