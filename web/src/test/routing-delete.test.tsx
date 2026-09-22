import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
	fireEvent,
	render,
	screen,
	waitFor,
	within,
} from "@testing-library/react";
import { AuthProvider } from "../auth/AuthContext";
import { I18nProvider } from "../i18n/I18nContext";
import { RoutingPage } from "../pages/RoutingPage";
import { HttpResponse, http, server } from "./server";

function renderRouting() {
	const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
	return render(
		<QueryClientProvider client={qc}>
			<AuthProvider>
				<I18nProvider>
					<RoutingPage />
				</I18nProvider>
			</AuthProvider>
		</QueryClientProvider>,
	);
}

describe("RoutingPage delete errors", () => {
	// #706: rule Delete confirms like every other destructive Panel action —
	// no DELETE until the dialog action is clicked.
	it("does not DELETE a rule until delete is confirmed", async () => {
		const deletes: string[] = [];
		server.use(
			http.get("/api/warp", () => HttpResponse.json({ enabled: true })),
			http.get("/api/routing/rules", () =>
				HttpResponse.json([
					{
						name: "warp-out",
						match: "geoip:cn",
						outbound: "warp",
						enabled: true,
					},
				]),
			),
			http.delete("/api/routing/rules/warp-out", () => {
				deletes.push("warp-out");
				return HttpResponse.json({ name: "warp-out", success: true });
			}),
		);
		renderRouting();
		fireEvent.click(await screen.findByRole("button", { name: /^delete$/i }));
		expect(await screen.findByRole("alertdialog")).toBeInTheDocument();
		expect(deletes).toEqual([]);
		fireEvent.click(screen.getByRole("button", { name: /confirm delete/i }));
		await waitFor(() => expect(deletes).toEqual(["warp-out"]));
	});

	it("shows an API error when delete fails", async () => {
		server.use(
			http.get("/api/warp", () => HttpResponse.json({ enabled: true })),
			http.get("/api/routing/rules", () =>
				HttpResponse.json([
					{
						name: "warp-out",
						match: "geoip:cn",
						outbound: "warp",
						enabled: true,
					},
				]),
			),
			http.delete("/api/routing/rules/warp-out", () =>
				HttpResponse.json(
					{ error: { message: "rule in use" } },
					{ status: 409 },
				),
			),
		);
		renderRouting();
		fireEvent.click(await screen.findByRole("button", { name: /^delete$/i }));
		fireEvent.click(
			await screen.findByRole("button", { name: /confirm delete/i }),
		);
		// The dialog stays open with the failure in context (the page card
		// behind it repeats the error) — assert it inside the dialog.
		const dialog = await screen.findByRole("alertdialog");
		expect(
			await within(dialog).findByText(/rule in use/i),
		).toBeInTheDocument();
	});

	// #644: a 200 with success=false means the delete committed but the
	// auto-apply failed — the page must say so, not silently remove the row.
	it("surfaces a committed-but-unapplied delete instead of silent removal", async () => {
		server.use(
			http.get("/api/warp", () => HttpResponse.json({ enabled: true })),
			http.get("/api/routing/rules", () =>
				HttpResponse.json([
					{
						name: "warp-out",
						match: "geoip:cn",
						outbound: "warp",
						enabled: true,
					},
				]),
			),
			http.delete("/api/routing/rules/warp-out", () =>
				HttpResponse.json({
					name: "warp-out",
					success: false,
					revision: { desired: 2, applied: 1, state: "drift" },
				}),
			),
		);
		renderRouting();
		fireEvent.click(await screen.findByRole("button", { name: /^delete$/i }));
		fireEvent.click(
			await screen.findByRole("button", { name: /confirm delete/i }),
		);
		expect(await screen.findByText(/applying it failed/i)).toBeInTheDocument();
	});

	// #644: a committed-but-unapplied save keeps the editor open with the
	// failure visible instead of dismissing it as a clean save.
	it("keeps the editor open when a rule save commits but the apply fails", async () => {
		server.use(
			http.get("/api/warp", () => HttpResponse.json({ enabled: true })),
			http.get("/api/routing/rules", () =>
				HttpResponse.json([
					{
						name: "warp-out",
						match: "geoip:cn",
						outbound: "warp",
						enabled: true,
					},
				]),
			),
			http.put("/api/routing/rules/warp-out", () =>
				HttpResponse.json({
					name: "warp-out",
					match: "geoip:cn",
					outbound: "warp",
					enabled: false,
					success: false,
					revision: { desired: 2, applied: 1, state: "drift" },
				}),
			),
		);
		renderRouting();
		fireEvent.click(await screen.findByRole("button", { name: /^edit$/i }));
		fireEvent.click(await screen.findByRole("button", { name: /^save$/i }));
		expect(await screen.findByText(/applying it failed/i)).toBeInTheDocument();
		expect(screen.getByRole("button", { name: /^save$/i })).toBeInTheDocument();
	});
});
