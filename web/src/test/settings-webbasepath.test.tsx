import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen } from "@testing-library/react";
import { AuthProvider } from "../auth/AuthContext";
import { I18nProvider } from "../i18n/I18nContext";
import { SettingsPage } from "../pages/SettingsPage";
import { HttpResponse, http, server } from "./server";

function renderSettings() {
	const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
	return render(
		<QueryClientProvider client={qc}>
			<AuthProvider>
				<I18nProvider>
					<SettingsPage />
				</I18nProvider>
			</AuthProvider>
		</QueryClientProvider>,
	);
}

describe("SettingsPage web base path", () => {
	it("does not report saved when the web base path is only cleared", async () => {
		const puts: unknown[] = [];
		server.use(
			http.get("/api/settings", () =>
				HttpResponse.json({
					domain: "example.test",
					mode: "dev",
					panelListen: "127.0.0.1:2096",
					panelAccess: "local",
					webBasePath: "/secret/",
				}),
			),
			http.put("/api/settings", async ({ request }) => {
				puts.push(await request.json());
				return HttpResponse.json({ success: true });
			}),
		);
		renderSettings();
		fireEvent.click(await screen.findByRole("button", { name: /^edit$/i }));
		const input = await screen.findByLabelText(/web base path/i);
		fireEvent.change(input, { target: { value: "" } });
		fireEvent.click(screen.getByRole("button", { name: /^save$/i }));
		expect(
			await screen.findByText(/web base path cannot be cleared/i),
		).toBeInTheDocument();
		expect(screen.queryByText(/settings saved/i)).not.toBeInTheDocument();
		expect(puts).toEqual([]);
	});

	it("clears a protocolFields-only naiveUsername instead of echoing the live value", async () => {
		const puts: Array<Record<string, unknown>> = [];
		server.use(
			http.get("/api/settings", () =>
				HttpResponse.json({
					mode: "prod",
					panelListen: "127.0.0.1:2096",
					protocolFields: { naiveUsername: "veil" },
				}),
			),
			http.put("/api/settings", async ({ request }) => {
				puts.push((await request.json()) as Record<string, unknown>);
				return HttpResponse.json({ success: true });
			}),
		);
		renderSettings();
		fireEvent.click(await screen.findByRole("button", { name: /^edit$/i }));
		const input = await screen.findByLabelText(/naiveproxy username/i);
		expect(input).toHaveValue("veil");
		fireEvent.change(input, { target: { value: "" } });
		fireEvent.click(screen.getByRole("button", { name: /^save$/i }));
		await screen.findByText(/settings saved/i);
		expect(puts).toHaveLength(1);
		expect(puts[0]?.naiveUsername).toBe("");
		expect(
			(puts[0]?.protocolFields as Record<string, unknown> | undefined)
				?.naiveUsername,
		).toBe("");
	});

	// #631: success=false on a committed PUT means the apply did not
	// converge — the notice must say that instead of "Settings saved.".
	it("surfaces a committed-but-unapplied save instead of claiming success", async () => {
		server.use(
			http.get("/api/settings", () =>
				HttpResponse.json({
					domain: "example.test",
					mode: "dev",
					panelListen: "127.0.0.1:2096",
				}),
			),
			http.put("/api/settings", () =>
				HttpResponse.json({
					success: false,
					revision: { desired: 2, applied: 1, state: "drift" },
				}),
			),
		);
		renderSettings();
		fireEvent.click(await screen.findByRole("button", { name: /^edit$/i }));
		const input = await screen.findByLabelText(/^domain$/i);
		fireEvent.change(input, { target: { value: "changed.example.test" } });
		fireEvent.click(screen.getByRole("button", { name: /^save$/i }));
		expect(
			await screen.findByText(/applying the new revision failed/i),
		).toBeInTheDocument();
		expect(screen.queryByText(/^settings saved\.$/i)).not.toBeInTheDocument();
	});
});
