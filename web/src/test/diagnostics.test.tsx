import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen } from "@testing-library/react";
import { AuthProvider } from "../auth/AuthContext";
import { I18nProvider } from "../i18n/I18nContext";
import { DiagnosticsPage } from "../pages/DiagnosticsPage";
import { HttpResponse, http, server } from "./server";

function renderDiagnostics(firewall: Record<string, unknown>) {
	server.use(
		http.get("/api/firewall", () => HttpResponse.json(firewall)),
		http.get("/api/audit", () => HttpResponse.json({ items: [] })),
		http.get("/api/status", () => HttpResponse.json({ services: [] })),
	);
	const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
	return render(
		<QueryClientProvider client={qc}>
			<AuthProvider>
				<I18nProvider initialLocale="en">
					<DiagnosticsPage />
				</I18nProvider>
			</AuthProvider>
		</QueryClientProvider>,
	);
}

describe("Diagnostics firewall response", () => {
	it("renders the backend active/rules envelope", async () => {
		renderDiagnostics({
			active: true,
			rules: [{ port: 443, protocol: "udp", service: "Hysteria2" }],
		});
		expect(await screen.findByText("Hysteria2")).toBeInTheDocument();
		expect(screen.getByText("443")).toBeInTheDocument();
	});

	it("accepts the backend null rule list when no ports are managed", async () => {
		renderDiagnostics({ active: false, rules: null });
		expect(await screen.findByText("No firewall rules.")).toBeInTheDocument();
	});

	it("shows a load error for a malformed rule list instead of crashing", async () => {
		renderDiagnostics({ active: true, rules: { port: 443 } });
		expect(
			await screen.findByText("Failed to load firewall rules"),
		).toBeInTheDocument();
		expect(screen.queryByText("No firewall rules.")).not.toBeInTheDocument();
	});
});
