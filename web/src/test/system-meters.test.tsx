import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
	createMemoryHistory,
	createRouter,
	RouterProvider,
} from "@tanstack/react-router";
import { render, screen } from "@testing-library/react";
import { AuthProvider } from "../auth/AuthContext";
import { I18nProvider } from "../i18n/I18nContext";
import { TrafficPage } from "../pages/TrafficPage";
import { routeTree } from "../routeTree.gen";
import { HttpResponse, http, server } from "./server";

function renderPath(path: string) {
	const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
	const router = createRouter({
		routeTree,
		history: createMemoryHistory({ initialEntries: [path] }),
	});
	return render(
		<QueryClientProvider client={qc}>
			<AuthProvider>
				<I18nProvider>
					<RouterProvider router={router} />
				</I18nProvider>
			</AuthProvider>
		</QueryClientProvider>,
	);
}

function renderTraffic() {
	const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
	return render(
		<QueryClientProvider client={qc}>
			<I18nProvider>
				<TrafficPage />
			</I18nProvider>
		</QueryClientProvider>,
	);
}

describe("CSP-safe meters and traffic chart", () => {
	it("renders system meters as progress elements with values, not inline widths", async () => {
		server.use(
			http.get("/api/system", () =>
				HttpResponse.json({
					cpuPercent: 42.2,
					memoryUsedMB: 512,
					memoryTotalMB: 1024,
					diskUsedGB: 10,
					diskTotalGB: 100,
					loadAvg1: 0.1,
					loadAvg5: 0.2,
					loadAvg15: 0.3,
					uptimeSeconds: 3600,
				}),
			),
		);
		renderPath("/system");
		const bars = await screen.findAllByRole("progressbar");
		expect(bars.length).toBeGreaterThanOrEqual(3);
		expect(bars[0]).toHaveAttribute("value", "42");
		expect(bars[0].tagName.toLowerCase()).toBe("progress");
		expect(bars[0]).not.toHaveAttribute("style");
	});

	it("gives the traffic chart a CSS class instead of an inline size", async () => {
		server.use(
			http.get("/api/v1/traffic/summary", () =>
				HttpResponse.json({
					state: "healthy",
					providerCount: 1,
					uploadBytes: 10,
					downloadBytes: 20,
					usedBytes: 30,
				}),
			),
			http.get("/api/v1/traffic/top", () =>
				HttpResponse.json({
					items: [
						{
							clientId: "c1",
							name: "Alice",
							uploadBytes: 10,
							downloadBytes: 20,
							totalBytes: 30,
						},
					],
				}),
			),
		);
		renderTraffic();
		await screen.findByText("Alice", {}, { timeout: 5_000 });
		const chart = document.querySelector(".traffic-chart");
		expect(chart).not.toBeNull();
		expect(chart).not.toHaveAttribute("style");
	});

	it("does not treat a failed usage breakdown as empty", async () => {
		server.use(
			http.get("/api/v1/traffic/summary", () =>
				HttpResponse.json({
					state: "healthy",
					providerCount: 1,
					uploadBytes: 10,
					downloadBytes: 20,
					usedBytes: 30,
				}),
			),
			http.get("/api/v1/traffic/top", () =>
				HttpResponse.json({ error: { message: "down" } }, { status: 500 }),
			),
		);
		renderTraffic();
		expect(
			await screen.findByText(/could not load usage breakdown/i),
		).toBeInTheDocument();
		expect(screen.queryByText(/no usage recorded/i)).not.toBeInTheDocument();
	});
});
