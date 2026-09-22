import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen } from "@testing-library/react";
import { I18nProvider } from "../i18n/I18nContext";
import { ClientTrafficPanel } from "../subscription/ClientTrafficPanel";
import { HttpResponse, http, server } from "./server";

function renderPanel() {
	const qc = new QueryClient({
		defaultOptions: { queries: { retry: false, refetchInterval: false } },
	});
	return render(
		<QueryClientProvider client={qc}>
			<I18nProvider>
				<ClientTrafficPanel clientId="c1" />
			</I18nProvider>
		</QueryClientProvider>,
	);
}

function trafficApi(body: Record<string, unknown>) {
	server.use(http.get("/api/v1/traffic/c1", () => HttpResponse.json(body)));
}

// #724: `collectedAt` is null until the first observation, and `state`
// distinguishes pending/unsupported/stale — zeros are not live usage.
describe("ClientTrafficPanel telemetry state", () => {
	it("shows 'unsupported' guidance instead of zeros + epoch when nothing can report", async () => {
		trafficApi({
			clientId: "c1",
			uploadBytes: 0,
			downloadBytes: 0,
			usedBytes: 0,
			depleted: false,
			state: "unsupported",
			collectedAt: null,
		});
		renderPanel();
		expect(await screen.findByText(/unsupported/i)).toBeInTheDocument();
		expect(screen.getByText(/no live usage to show/i)).toBeInTheDocument();
		expect(screen.queryByText(/^collected /i)).not.toBeInTheDocument();
		// No fake counters and no 12:00:00 AM epoch timestamp.
		expect(screen.queryByText(/^upload$/i)).not.toBeInTheDocument();
	});

	it("shows 'pending' guidance when accounting exists but no sample arrived", async () => {
		trafficApi({
			clientId: "c1",
			uploadBytes: 0,
			downloadBytes: 0,
			usedBytes: 0,
			depleted: false,
			state: "pending",
			collectedAt: null,
		});
		renderPanel();
		expect(
			await screen.findByText(/pending first observation/i),
		).toBeInTheDocument();
		expect(
			screen.getByText(/no sample has been observed yet/i),
		).toBeInTheDocument();
		expect(screen.queryByText(/^collected /i)).not.toBeInTheDocument();
	});

	it("renders counters with a stale badge when providers are degraded", async () => {
		trafficApi({
			clientId: "c1",
			uploadBytes: 1024,
			downloadBytes: 2048,
			usedBytes: 3072,
			depleted: false,
			state: "stale",
			collectedAt: 1700000000,
		});
		renderPanel();
		expect(await screen.findByText(/^stale$/i)).toBeInTheDocument();
		expect(screen.getByText(/3.0 KiB/i)).toBeInTheDocument();
		expect(screen.getByText(/^collected /i)).toBeInTheDocument();
	});

	it("renders healthy counters with the collection time", async () => {
		trafficApi({
			clientId: "c1",
			uploadBytes: 1024,
			downloadBytes: 2048,
			usedBytes: 3072,
			depleted: false,
			state: "healthy",
			collectedAt: 1700000000,
		});
		renderPanel();
		expect(await screen.findByText(/^healthy$/i)).toBeInTheDocument();
		expect(screen.getByText(/3.0 KiB/i)).toBeInTheDocument();
		expect(screen.getByText(/^collected /i)).toBeInTheDocument();
	});
});
