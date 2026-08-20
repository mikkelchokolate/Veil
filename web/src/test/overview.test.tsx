import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
	createRootRoute,
	createRoute,
	createRouter,
	RouterProvider,
} from "@tanstack/react-router";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { PanelRestartTimeoutError } from "../api/panelUpdate";
import { AuthProvider } from "../auth/AuthContext";
import { I18nProvider } from "../i18n/I18nContext";
import { OverviewPage } from "../pages/OverviewPage";
import { HttpResponse, http, server } from "./server";

const panelUpdateMocks = vi.hoisted(() => ({
	waitForPanelVersion: vi.fn(),
	reloadPanel: vi.fn(),
}));

vi.mock("../api/panelUpdate", async (importOriginal) => {
	const actual = await importOriginal<typeof import("../api/panelUpdate")>();
	return {
		...actual,
		waitForPanelVersion: panelUpdateMocks.waitForPanelVersion,
		reloadPanel: panelUpdateMocks.reloadPanel,
	};
});

function renderOverview() {
	const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
	const rootRoute = createRootRoute();
	const route = createRoute({
		getParentRoute: () => rootRoute,
		path: "/",
		component: OverviewPage,
	});
	const router = createRouter({
		routeTree: rootRoute.addChildren([route]),
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

function overviewApis(role: "admin" | "viewer" = "admin") {
	server.use(
		http.get("/api/auth/status", () =>
			HttpResponse.json({
				authenticated: true,
				username: role,
				role,
				csrfToken: "test-csrf",
			}),
		),
		http.get("/api/version", () =>
			HttpResponse.json({
				version: "v0.6.3-test",
				runtime: "linux/amd64",
				name: "Veil",
			}),
		),
		http.get("/api/system", () =>
			HttpResponse.json({
				cpuPercent: 1.2,
				memoryUsedMB: 100,
				memoryTotalMB: 1024,
				uptimeSeconds: 3600,
			}),
		),
		http.get("/api/v1/clients", () =>
			HttpResponse.json({ items: [], total: 3, page: 1, pageSize: 1 }),
		),
	);
}

async function confirmUpdate() {
	fireEvent.click(screen.getByRole("button", { name: "Update panel" }));
	fireEvent.click(await screen.findByRole("button", { name: "Start update" }));
}

afterEach(() => {
	panelUpdateMocks.waitForPanelVersion.mockReset();
	panelUpdateMocks.reloadPanel.mockReset();
});

describe("OverviewPage version", () => {
	it("shows the installed panel version and runtime", async () => {
		overviewApis();
		renderOverview();
		await waitFor(() =>
			expect(screen.getByTestId("panel-version")).toHaveTextContent(
				"v0.6.3-test",
			),
		);
		expect(screen.getByText("linux/amd64")).toBeInTheDocument();
		expect(
			screen.getByRole("button", { name: "Update panel" }),
		).toBeInTheDocument();
	});

	it("hides the update button from viewers", async () => {
		overviewApis("viewer");
		renderOverview();
		await waitFor(() =>
			expect(screen.getByTestId("panel-version")).toBeInTheDocument(),
		);
		expect(
			screen.queryByRole("button", { name: "Update panel" }),
		).not.toBeInTheDocument();
	});

	it("reloads after a successful staged update", async () => {
		overviewApis();
		panelUpdateMocks.waitForPanelVersion.mockResolvedValue({
			version: "v0.6.4",
			runtime: "linux/amd64",
			name: "Veil",
		});
		server.use(
			http.post("/api/version/update", () =>
				HttpResponse.json(
					{
						jobId: "job-1",
						status: "restart_pending",
						staged: true,
						installed: true,
						version: "v0.6.4",
						message:
							"Update installed; durable restart verification is pending.",
					},
					{ status: 202 },
				),
			),
		);
		renderOverview();
		await waitFor(() =>
			expect(
				screen.getByRole("button", { name: "Update panel" }),
			).toBeEnabled(),
		);
		await confirmUpdate();
		await waitFor(() =>
			expect(panelUpdateMocks.waitForPanelVersion).toHaveBeenCalled(),
		);
		await waitFor(() =>
			expect(panelUpdateMocks.reloadPanel).toHaveBeenCalled(),
		);
	});

	it("shows the API error when staging fails", async () => {
		overviewApis();
		server.use(
			http.post("/api/version/update", () =>
				HttpResponse.json(
					{
						error: "privileged helper is unavailable",
					},
					{ status: 503 },
				),
			),
		);
		renderOverview();
		await waitFor(() =>
			expect(
				screen.getByRole("button", { name: "Update panel" }),
			).toBeEnabled(),
		);
		await confirmUpdate();
		await waitFor(() =>
			expect(
				screen.getByText(/privileged helper is unavailable/i),
			).toBeInTheDocument(),
		);
		expect(panelUpdateMocks.waitForPanelVersion).not.toHaveBeenCalled();
		expect(panelUpdateMocks.reloadPanel).not.toHaveBeenCalled();
	});

	it("asks the operator to refresh if restart polling times out", async () => {
		overviewApis();
		panelUpdateMocks.waitForPanelVersion.mockRejectedValue(
			new PanelRestartTimeoutError(),
		);
		server.use(
			http.post("/api/version/update", () =>
				HttpResponse.json(
					{
						jobId: "job-1",
						status: "restart_pending",
						staged: true,
						installed: true,
						version: "v0.6.4",
					},
					{ status: 202 },
				),
			),
		);
		renderOverview();
		await waitFor(() =>
			expect(
				screen.getByRole("button", { name: "Update panel" }),
			).toBeEnabled(),
		);
		await confirmUpdate();
		await waitFor(() =>
			expect(
				screen.getByText(/refresh the page in a few seconds/i),
			).toBeInTheDocument(),
		);
		expect(panelUpdateMocks.reloadPanel).not.toHaveBeenCalled();
	});
});
