import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
	createRootRoute,
	createRoute,
	createRouter,
	RouterProvider,
} from "@tanstack/react-router";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import {
	PanelRestartTimeoutError,
	PanelUpdateFailedError,
} from "../api/panelUpdate";
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

function renderOverview(locale: "en" | "ru" = "en") {
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
				<I18nProvider initialLocale={locale}>
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

// #691: the badge colors from the API state, not revision equality — a
// failed equal-revision apply reports degraded server-side (#543) and must
// never render green; untracked (#539) must not render green either.
describe("OverviewPage apply state badge", () => {
	it("renders a degraded equal-revision state as danger, not green", async () => {
		overviewApis();
		server.use(
			http.get("/api/apply/state", () =>
				HttpResponse.json({
					desiredRevision: 2,
					appliedRevision: 2,
					state: "degraded",
				}),
			),
		);
		renderOverview();
		const badge = await screen.findByText("Degraded");
		expect(badge.className).toContain("danger");
		expect(badge.className).not.toContain("success");
	});

	it("renders untracked as a warning instead of green", async () => {
		overviewApis();
		server.use(
			http.get("/api/apply/state", () =>
				HttpResponse.json({
					desiredRevision: 0,
					appliedRevision: 0,
					state: "untracked",
				}),
			),
		);
		renderOverview();
		const badge = await screen.findByText("Not tracked");
		expect(badge.className).toContain("warning");
		expect(badge.className).not.toContain("success");
	});

	it("renders synced as success", async () => {
		overviewApis();
		server.use(
			http.get("/api/apply/state", () =>
				HttpResponse.json({
					desiredRevision: 2,
					appliedRevision: 2,
					state: "synced",
				}),
			),
		);
		renderOverview();
		const badge = await screen.findByText("Synced");
		expect(badge.className).toContain("success");
	});

	// #703: the label comes from applyState.* — in ru the badge is the
	// translated string, not the raw English enum.
	it("localizes the apply-state badge in ru", async () => {
		overviewApis();
		server.use(
			http.get("/api/apply/state", () =>
				HttpResponse.json({
					desiredRevision: 2,
					appliedRevision: 2,
					state: "degraded",
				}),
			),
		);
		renderOverview("ru");
		const badge = await screen.findByText("Деградировано");
		expect(badge.className).toContain("danger");
	});
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

	// #813: "Update panel" only opens the confirm dialog — the staging POST
	// must not fire until the dialog action is clicked.
	it("does not POST the update until the dialog action", async () => {
		overviewApis();
		const posts: string[] = [];
		server.use(
			http.post("/api/version/update", () => {
				posts.push("update");
				return HttpResponse.json(
					{
						jobId: "job-1",
						status: "restart_pending",
						staged: true,
						installed: true,
						version: "v0.6.4",
					},
					{ status: 202 },
				);
			}),
		);
		renderOverview();
		await waitFor(() =>
			expect(
				screen.getByRole("button", { name: "Update panel" }),
			).toBeEnabled(),
		);
		fireEvent.click(screen.getByRole("button", { name: "Update panel" }));
		expect(await screen.findByRole("alertdialog")).toBeInTheDocument();
		expect(posts).toEqual([]);
		fireEvent.click(screen.getByRole("button", { name: "Start update" }));
		await waitFor(() => expect(posts).toEqual(["update"]));
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
			expect(panelUpdateMocks.waitForPanelVersion).toHaveBeenCalledWith(
				expect.objectContaining({
					previousVersion: "v0.6.3-test",
					expectedVersion: "v0.6.4",
					jobId: "job-1",
				}),
			),
		);
		await waitFor(() =>
			expect(panelUpdateMocks.reloadPanel).toHaveBeenCalled(),
		);
	});

	it("does not reload while restart polling is still waiting on the old binary", async () => {
		overviewApis();
		panelUpdateMocks.waitForPanelVersion.mockReturnValue(
			new Promise(() => undefined),
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
			expect(panelUpdateMocks.waitForPanelVersion).toHaveBeenCalled(),
		);
		expect(
			await screen.findByText(/waiting for the panel service to restart/i),
		).toBeInTheDocument();
		expect(panelUpdateMocks.reloadPanel).not.toHaveBeenCalled();
	});

	it("shows a job failure without reloading", async () => {
		overviewApis();
		panelUpdateMocks.waitForPanelVersion.mockRejectedValue(
			new PanelUpdateFailedError("helper refused restart"),
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
			expect(screen.getByText(/helper refused restart/i)).toBeInTheDocument(),
		);
		expect(panelUpdateMocks.reloadPanel).not.toHaveBeenCalled();
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

	it("does not hide a failed client count or system vitals as a dash", async () => {
		overviewApis();
		server.use(
			http.get("/api/apply/state", () =>
				HttpResponse.json({ error: { message: "down" } }, { status: 500 }),
			),
			http.get("/api/v1/clients", () =>
				HttpResponse.json({ error: { message: "down" } }, { status: 500 }),
			),
			http.get("/api/system", () =>
				HttpResponse.json({ error: { message: "down" } }, { status: 500 }),
			),
		);
		renderOverview();
		expect(
			await screen.findByText(/could not load client count/i),
		).toBeInTheDocument();
		expect(
			screen.getByText(/could not load system vitals/i),
		).toBeInTheDocument();
		expect(screen.getByText(/apply state unavailable/i)).toBeInTheDocument();
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
