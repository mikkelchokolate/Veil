import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { I18nProvider } from "../i18n/I18nContext";
import { HttpResponse, http, server } from "./server";

const echartsMocks = vi.hoisted(() => {
	type Chart = {
		el: HTMLElement;
		disposed: boolean;
		setOption: ReturnType<typeof vi.fn>;
		resize: ReturnType<typeof vi.fn>;
		dispose: ReturnType<typeof vi.fn>;
	};
	const instances: Chart[] = [];
	return {
		instances,
		init: vi.fn((el: HTMLElement) => {
			const instance: Chart = {
				el,
				disposed: false,
				setOption: vi.fn(),
				resize: vi.fn(),
				dispose: vi.fn(() => {
					instance.disposed = true;
				}),
			};
			instances.push(instance);
			return instance;
		}),
		use: vi.fn(),
	};
});

vi.mock("echarts/core", () => ({
	use: echartsMocks.use,
	init: echartsMocks.init,
}));
vi.mock("echarts/charts", () => ({ BarChart: {} }));
vi.mock("echarts/components", () => ({
	GridComponent: {},
	LegendComponent: {},
	TooltipComponent: {},
}));
vi.mock("echarts/renderers", () => ({ CanvasRenderer: {} }));

import { TrafficPage } from "../pages/TrafficPage";

const alice = {
	clientId: "c1",
	name: "Alice",
	uploadBytes: 10,
	downloadBytes: 20,
	totalBytes: 30,
};

type Talker = {
	clientId: string;
	name: string;
	uploadBytes?: number;
	downloadBytes?: number;
	totalBytes?: number;
	usedBytes?: number;
};

function trafficApis(opts: {
	providerCount: number;
	items: Talker[];
	topError?: boolean;
}) {
	server.use(
		http.get("/api/v1/traffic/summary", () =>
			HttpResponse.json({
				state: "healthy",
				providerCount: opts.providerCount,
				uploadBytes: 10,
				downloadBytes: 20,
				usedBytes: 30,
			}),
		),
		http.get("/api/v1/traffic/top", () => {
			if (opts.topError) {
				return HttpResponse.json(
					{ error: { message: "down" } },
					{ status: 500 },
				);
			}
			return HttpResponse.json({ items: opts.items });
		}),
	);
}

function renderTraffic() {
	const qc = new QueryClient({
		defaultOptions: { queries: { retry: false, refetchInterval: false } },
	});
	const view = render(
		<QueryClientProvider client={qc}>
			<I18nProvider>
				<TrafficPage />
			</I18nProvider>
		</QueryClientProvider>,
	);
	return { ...view, qc };
}

async function waitForChart() {
	await waitFor(() => expect(echartsMocks.init).toHaveBeenCalled());
	expect(document.querySelector(".traffic-chart")).not.toBeNull();
}

describe("Traffic chart instance lifecycle", () => {
	beforeEach(() => {
		echartsMocks.instances.length = 0;
		echartsMocks.init.mockClear();
		Object.defineProperty(HTMLElement.prototype, "clientWidth", {
			configurable: true,
			get() {
				return 640;
			},
		});
		Object.defineProperty(HTMLElement.prototype, "clientHeight", {
			configurable: true,
			get() {
				return 320;
			},
		});
	});

	it("disposes the detached chart and inits a replacement after empty data recovers", async () => {
		trafficApis({ providerCount: 1, items: [alice] });
		const { qc } = renderTraffic();
		await screen.findByText("Alice");
		await waitForChart();
		const first = echartsMocks.instances[0];
		expect(first?.disposed).toBe(false);
		const firstEl = first?.el;

		trafficApis({ providerCount: 1, items: [] });
		await qc.invalidateQueries({ queryKey: ["traffic"] });
		await waitFor(() =>
			expect(screen.getByText(/no usage recorded/i)).toBeInTheDocument(),
		);
		expect(document.querySelector(".traffic-chart")).toBeNull();
		expect(first?.disposed).toBe(true);

		trafficApis({ providerCount: 1, items: [alice] });
		await qc.invalidateQueries({ queryKey: ["traffic"] });
		await screen.findByText("Alice");
		await waitFor(() => expect(echartsMocks.instances.length).toBe(2));
		const second = echartsMocks.instances[1];
		expect(first?.disposed).toBe(true);
		expect(second?.disposed).toBe(false);
		expect(second?.el).not.toBe(firstEl);
		expect(second?.el).toBe(document.querySelector(".traffic-chart"));
	});

	it("reinitializes the chart after a top-list error recovers", async () => {
		trafficApis({ providerCount: 1, items: [alice] });
		const { qc } = renderTraffic();
		await screen.findByText("Alice");
		await waitForChart();
		const first = echartsMocks.instances[0];

		trafficApis({ providerCount: 1, items: [alice], topError: true });
		await qc.invalidateQueries({ queryKey: ["traffic"] });
		await waitFor(() =>
			expect(
				screen.getByText(/could not load usage breakdown/i),
			).toBeInTheDocument(),
		);
		expect(first?.disposed).toBe(true);

		trafficApis({ providerCount: 1, items: [alice] });
		await qc.invalidateQueries({ queryKey: ["traffic"] });
		await screen.findByText("Alice");
		await waitFor(() => expect(echartsMocks.instances.length).toBe(2));
		expect(echartsMocks.instances[1]?.disposed).toBe(false);
		expect(echartsMocks.instances[1]?.el).toBe(
			document.querySelector(".traffic-chart"),
		);
	});

	it("reinitializes the chart when telemetry disappears and returns", async () => {
		trafficApis({ providerCount: 1, items: [alice] });
		const { qc } = renderTraffic();
		await screen.findByText("Alice");
		await waitForChart();
		const first = echartsMocks.instances[0];

		trafficApis({ providerCount: 0, items: [] });
		await qc.invalidateQueries({ queryKey: ["traffic"] });
		await waitFor(() =>
			expect(screen.getByText(/no traffic source/i)).toBeInTheDocument(),
		);
		expect(first?.disposed).toBe(true);

		trafficApis({ providerCount: 1, items: [alice] });
		await qc.invalidateQueries({ queryKey: ["traffic"] });
		await screen.findByText("Alice");
		await waitFor(() => expect(echartsMocks.instances.length).toBe(2));
		expect(echartsMocks.instances[1]?.el).toBe(
			document.querySelector(".traffic-chart"),
		);
	});

	it("escapes client names before they land in the tooltip HTML", async () => {
		const payload = "<img src=x onerror=alert(1)>";
		trafficApis({
			providerCount: 1,
			items: [
				{
					clientId: "c1",
					name: payload,
					uploadBytes: 10,
					downloadBytes: 20,
				},
			],
		});
		renderTraffic();
		await waitForChart();
		const chart = echartsMocks.instances[0];
		const option = chart?.setOption.mock.calls.at(-1)?.[0] as {
			tooltip: { formatter: (p: unknown) => string };
		};
		const html = option.tooltip.formatter([
			{ name: payload, value: 10, seriesName: "Upload" },
			{ name: payload, value: 20, seriesName: "Download" },
		]);
		expect(html).not.toContain("<img");
		expect(html).toContain("&lt;img");
	});

	it("renders Total from usedBytes when the live API omits totalBytes", async () => {
		trafficApis({
			providerCount: 1,
			items: [
				{
					clientId: "c1",
					name: "Alice",
					uploadBytes: 10,
					downloadBytes: 20,
					usedBytes: 30,
				},
			],
		});
		renderTraffic();
		await screen.findByText("Alice");
		const rows = screen.getAllByRole("row");
		const aliceRow = rows.find((row) => row.textContent?.includes("Alice"));
		expect(aliceRow).toHaveTextContent("30 B");
		expect(aliceRow?.textContent).not.toMatch(/—/);
	});
});
