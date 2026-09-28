import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor, within } from "@testing-library/react";
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

function summaryApi(providerCount = 0, topItems: unknown[] = []) {
	server.use(
		http.get("/api/v1/traffic/summary", () =>
			HttpResponse.json({
				state: providerCount > 0 ? "healthy" : "unsupported",
				providerCount,
				uploadBytes: 10,
				downloadBytes: 20,
				usedBytes: 30,
			}),
		),
		http.get("/api/v1/traffic/top", () =>
			HttpResponse.json({ items: topItems }),
		),
	);
}

function renderTraffic() {
	const qc = new QueryClient({
		defaultOptions: { queries: { retry: false, refetchInterval: false } },
	});
	return render(
		<QueryClientProvider client={qc}>
			<I18nProvider>
				<TrafficPage />
			</I18nProvider>
		</QueryClientProvider>,
	);
}

/** Locate the card div that owns a section heading so table assertions can be
 * scoped — the page now renders several tables. */
async function cardWithHeading(name: string | RegExp): Promise<HTMLElement> {
	const heading = await screen.findByRole("heading", { name });
	const card = heading.closest(".card");
	expect(card).not.toBeNull();
	return card as HTMLElement;
}

describe("Traffic page presence card", () => {
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

	it("renders online, offline, and unsupported clients honestly", async () => {
		summaryApi();
		server.use(
			http.get("/api/v1/presence", () =>
				HttpResponse.json({
					items: [
						{
							clientId: "c1",
							name: "Alice",
							online: true,
							source: "stats",
							connections: 2,
							lastActiveAt: 1700000000,
						},
						{
							clientId: "c2",
							name: "Bob",
							online: false,
							source: "activity",
							lastActiveAt: 1699990000,
						},
						{
							clientId: "c3",
							name: "Carol",
							online: null,
							source: "unsupported",
						},
					],
					count: 3,
				}),
			),
		);
		renderTraffic();
		const card = await cardWithHeading("Client presence");
		const table = await within(card).findByRole("table");
		expect(
			within(table).getByRole("columnheader", { name: "Status" }),
		).toBeInTheDocument();
		const rows = within(table).getAllByRole("row");
		expect(rows).toHaveLength(4);

		const alice = within(rows[1]).getAllByRole("cell");
		expect(alice[0]).toHaveTextContent("Alice");
		expect(alice[1]).toHaveTextContent("online");
		expect(alice[1]).not.toHaveTextContent("offline");
		expect(alice[3]).toHaveTextContent("2");
		expect(alice[4]).toHaveTextContent(
			new Date(1700000000 * 1000).toLocaleString(),
		);

		const bob = within(rows[2]).getAllByRole("cell");
		expect(bob[0]).toHaveTextContent("Bob");
		expect(bob[1]).toHaveTextContent("offline");

		// online=null means no telemetry source can prove either verdict — it
		// must render as "no telemetry", never as a fake "offline".
		const carol = within(rows[3]).getAllByRole("cell");
		expect(carol[0]).toHaveTextContent("Carol");
		expect(carol[1]).toHaveTextContent("no telemetry");
		expect(carol[1]).not.toHaveTextContent("offline");
		expect(carol[3]).toHaveTextContent("—");
		expect(carol[4]).toHaveTextContent("—");
	});

	it("shows an empty state when no clients report presence", async () => {
		summaryApi();
		renderTraffic();
		const card = await cardWithHeading("Client presence");
		expect(
			await within(card).findByText("No clients to report presence for."),
		).toBeInTheDocument();
		expect(within(card).queryByRole("table")).not.toBeInTheDocument();
	});

	it("surfaces a presence failure while other sections keep working", async () => {
		summaryApi();
		server.use(
			http.get("/api/v1/presence", () =>
				HttpResponse.json(
					{ error: { message: "presence down" } },
					{ status: 500 },
				),
			),
		);
		renderTraffic();
		const card = await cardWithHeading("Client presence");
		expect(await within(card).findByRole("alert")).toHaveTextContent(
			"presence down",
		);
		// The listeners and history cards fail independently of presence.
		const listeners = await cardWithHeading("Listeners");
		expect(
			within(listeners).getByText("No listeners detected."),
		).toBeInTheDocument();
	});
});

describe("Traffic page listeners card", () => {
	beforeEach(() => {
		echartsMocks.instances.length = 0;
		echartsMocks.init.mockClear();
	});

	it("renders listeners sorted by port with a process fallback", async () => {
		summaryApi();
		server.use(
			http.get("/api/connections", () =>
				HttpResponse.json({
					listeners: [
						{ proto: "tcp", address: "0.0.0.0", port: 8443 },
						{
							proto: "udp",
							address: "::",
							port: 2053,
							process: "veil-xray",
						},
						{ proto: "tcp", address: "127.0.0.1", port: 2096, process: "veil" },
					],
				}),
			),
		);
		renderTraffic();
		const card = await cardWithHeading("Listeners");
		const table = await within(card).findByRole("table");
		for (const name of ["Protocol", "Address", "Port", "Process"]) {
			expect(
				within(table).getByRole("columnheader", { name }),
			).toBeInTheDocument();
		}
		const rows = within(table).getAllByRole("row");
		expect(rows).toHaveLength(4);
		// Port-ascending: 2053, 2096, 8443 — not the raw payload order.
		const first = within(rows[1]).getAllByRole("cell");
		expect(first[0]).toHaveTextContent("udp");
		expect(first[2]).toHaveTextContent("2053");
		expect(first[3]).toHaveTextContent("veil-xray");
		const third = within(rows[3]).getAllByRole("cell");
		expect(third[2]).toHaveTextContent("8443");
		// Missing process renders an em-dash, never a blank cell.
		expect(third[3]).toHaveTextContent("—");
	});

	it("shows an empty state when no listeners are detected", async () => {
		summaryApi();
		renderTraffic();
		const card = await cardWithHeading("Listeners");
		expect(
			await within(card).findByText("No listeners detected."),
		).toBeInTheDocument();
		expect(within(card).queryByRole("table")).not.toBeInTheDocument();
	});

	it("surfaces a connections failure without hiding the presence card", async () => {
		summaryApi();
		server.use(
			http.get("/api/connections", () =>
				HttpResponse.json({ error: { message: "conn down" } }, { status: 500 }),
			),
			http.get("/api/v1/presence", () =>
				HttpResponse.json({
					items: [
						{
							clientId: "c1",
							name: "Alice",
							online: true,
							source: "stats",
							connections: 1,
						},
					],
					count: 1,
				}),
			),
		);
		renderTraffic();
		const card = await cardWithHeading("Listeners");
		expect(await within(card).findByRole("alert")).toHaveTextContent(
			"conn down",
		);
		const presence = await cardWithHeading("Client presence");
		expect(within(presence).getByText("Alice")).toBeInTheDocument();
	});
});

describe("Traffic page history card", () => {
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

	it("renders bucketed deltas as a stacked bar chart, time-ascending", async () => {
		summaryApi();
		server.use(
			http.get("/api/v1/traffic/history", () =>
				HttpResponse.json({
					// Delivered out of order on purpose — the page must sort.
					items: [
						{
							bucketStart: 1700000060,
							clientId: "",
							bindingId: "",
							uploadDelta: 512,
							downloadDelta: 4096,
						},
						{
							bucketStart: 1700000000,
							clientId: "",
							bindingId: "",
							uploadDelta: 1024,
							downloadDelta: 2048,
						},
					],
					count: 2,
				}),
			),
		);
		renderTraffic();
		await cardWithHeading("Traffic history");
		await waitFor(() => expect(echartsMocks.init).toHaveBeenCalled());
		// providerCount=0 hides the usage chart — the single instance is the
		// history chart.
		expect(echartsMocks.instances).toHaveLength(1);
		const option = echartsMocks.instances[0]?.setOption.mock.calls.at(
			-1,
		)?.[0] as {
			xAxis: { data: string[] };
			series: Array<{ name: string; data: number[]; stack?: string }>;
		};
		expect(option.xAxis.data).toHaveLength(2);
		expect(option.series.map((s) => s.name)).toEqual(["Upload", "Download"]);
		expect(option.series[0]?.data).toEqual([1024, 512]);
		expect(option.series[1]?.data).toEqual([2048, 4096]);
	});

	it("shows an empty state when no history is recorded", async () => {
		summaryApi();
		renderTraffic();
		const card = await cardWithHeading("Traffic history");
		expect(
			await within(card).findByText("No history recorded yet."),
		).toBeInTheDocument();
		expect(echartsMocks.init).not.toHaveBeenCalled();
	});

	it("surfaces a history failure as a card-level error", async () => {
		summaryApi();
		server.use(
			http.get("/api/v1/traffic/history", () =>
				HttpResponse.json(
					{ error: { message: "history down" } },
					{ status: 500 },
				),
			),
		);
		renderTraffic();
		const card = await cardWithHeading("Traffic history");
		expect(await within(card).findByRole("alert")).toHaveTextContent(
			"history down",
		);
	});

	it("keeps the usage breakdown card when telemetry exists", async () => {
		summaryApi(1, [
			{
				clientId: "c1",
				name: "Alice",
				uploadBytes: 10,
				downloadBytes: 20,
				usedBytes: 30,
			},
		]);
		renderTraffic();
		const card = await cardWithHeading("Usage breakdown");
		expect(await within(card).findByText("Alice")).toBeInTheDocument();
		await waitFor(() => expect(echartsMocks.instances.length).toBe(1));
	});
});
