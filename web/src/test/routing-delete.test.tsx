import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen } from "@testing-library/react";
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
		expect(await screen.findByText(/rule in use/i)).toBeInTheDocument();
	});
});
