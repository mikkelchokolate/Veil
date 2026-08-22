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
});
