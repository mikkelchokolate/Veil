import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, within } from "@testing-library/react";
import { I18nProvider } from "../i18n/I18nContext";
import { SystemPage } from "../pages/SystemPage";
import { HttpResponse, http, server } from "./server";

const systemStats = {
	cpuPercent: 42.2,
	memoryUsedMB: 512,
	memoryTotalMB: 1024,
	diskUsedGB: 10,
	diskTotalGB: 100,
	loadAvg1: 0.1,
	loadAvg5: 0.2,
	loadAvg15: 0.3,
	uptimeSeconds: 3600,
};

function renderSystem() {
	const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
	return render(
		<QueryClientProvider client={qc}>
			<I18nProvider>
				<SystemPage />
			</I18nProvider>
		</QueryClientProvider>,
	);
}

describe("System page processes section", () => {
	it("renders managed processes sorted by name with formatted values", async () => {
		server.use(
			http.get("/api/system", () => HttpResponse.json(systemStats)),
			http.get("/api/processes", () =>
				HttpResponse.json({
					processes: [
						{
							pid: 430,
							name: "veil-xray",
							cpuPercent: 12.34,
							memoryMB: 256,
							uptimeSeconds: 5400,
						},
						{
							pid: 123,
							name: "veil-agent",
							cpuPercent: 0.49,
							memoryMB: 32,
							uptimeSeconds: 65,
						},
					],
				}),
			),
		);
		renderSystem();
		const table = await screen.findByRole("table");
		expect(
			within(table).getByRole("columnheader", { name: "PID" }),
		).toBeInTheDocument();
		const rows = within(table).getAllByRole("row");
		// Header row + two data rows, name-ascending: veil-agent before veil-xray.
		expect(rows).toHaveLength(3);
		const first = within(rows[1]).getAllByRole("cell");
		expect(first[0]).toHaveTextContent("veil-agent");
		expect(first[1]).toHaveTextContent("123");
		expect(first[2]).toHaveTextContent("0.5%");
		expect(first[3]).toHaveTextContent("32 MiB");
		expect(first[4]).toHaveTextContent("1m");
		const second = within(rows[2]).getAllByRole("cell");
		expect(second[0]).toHaveTextContent("veil-xray");
		expect(second[2]).toHaveTextContent("12.3%");
		expect(second[4]).toHaveTextContent("1h 30m");
	});

	it("shows an empty state when no managed processes are running", async () => {
		server.use(
			http.get("/api/system", () => HttpResponse.json(systemStats)),
			http.get("/api/processes", () => HttpResponse.json({ processes: [] })),
		);
		renderSystem();
		expect(
			await screen.findByText("No managed processes."),
		).toBeInTheDocument();
		expect(screen.queryByRole("table")).not.toBeInTheDocument();
	});

	it("surfaces a processes fetch failure without hiding host metrics", async () => {
		server.use(
			http.get("/api/system", () => HttpResponse.json(systemStats)),
			http.get("/api/processes", () =>
				HttpResponse.json({ error: { message: "down" } }, { status: 500 }),
			),
		);
		renderSystem();
		// The 500 envelope carries a server message that apiFetch surfaces as-is.
		expect(await screen.findByRole("alert")).toHaveTextContent("down");
		// Host metrics keep rendering — the section fails independently.
		expect(
			(await screen.findAllByRole("progressbar")).length,
		).toBeGreaterThanOrEqual(3);
	});

	it("keeps the processes section when host metrics fail", async () => {
		server.use(
			http.get("/api/system", () =>
				HttpResponse.json({ error: { message: "host down" } }, { status: 500 }),
			),
			http.get("/api/processes", () =>
				HttpResponse.json({
					processes: [
						{
							pid: 123,
							name: "veil-agent",
							cpuPercent: 0.49,
							memoryMB: 32,
							uptimeSeconds: 65,
						},
					],
				}),
			),
		);
		renderSystem();
		// Host card reports its own failure…
		expect(await screen.findByRole("alert")).toHaveTextContent("host down");
		// …while the processes table still renders.
		const table = await screen.findByRole("table");
		expect(within(table).getByText("veil-agent")).toBeInTheDocument();
	});
});
