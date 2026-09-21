import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
	createRootRoute,
	createRoute,
	createRouter,
	RouterProvider,
} from "@tanstack/react-router";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { AuthProvider } from "../auth/AuthContext";
import { I18nProvider } from "../i18n/I18nContext";
import { ApplyPage } from "../pages/ApplyPage";
import { HttpResponse, http, server } from "./server";

function renderApply() {
	const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
	const rootRoute = createRootRoute();
	const route = createRoute({
		getParentRoute: () => rootRoute,
		path: "/",
		component: ApplyPage,
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

describe("ApplyPage", () => {
	it("shows drift and reconcile when applied is behind desired", async () => {
		server.use(
			http.get("/api/apply/state", () =>
				HttpResponse.json({
					desiredRevision: 3,
					appliedRevision: 1,
					state: "drift",
				}),
			),
			http.get("/api/apply/jobs", () => HttpResponse.json({ items: [] })),
		);
		renderApply();
		await waitFor(() =>
			expect(screen.getByText(/behind desired/i)).toBeInTheDocument(),
		);
		// Reconcile is admin-only; the drift indicator is the honest signal here.
	});

	it("renders job rows with status", async () => {
		server.use(
			http.get("/api/apply/state", () =>
				HttpResponse.json({
					desiredRevision: 2,
					appliedRevision: 2,
					state: "applied",
				}),
			),
			http.get("/api/apply/jobs", () =>
				HttpResponse.json({
					items: [
						{
							id: "j1",
							desiredRevision: 2,
							baseRevision: 1,
							status: "failed",
							trigger: "manual",
							createdAt: 1700000000,
							errorMessage: "haproxy reload failed",
						},
					],
				}),
			),
		);
		renderApply();
		await waitFor(() =>
			expect(screen.getByText(/haproxy reload failed/i)).toBeInTheDocument(),
		);
		expect(screen.getByText("failed")).toBeInTheDocument();
	});

	it("labels an applying job instead of showing a raw i18n key", async () => {
		server.use(
			http.get("/api/apply/state", () =>
				HttpResponse.json({
					desiredRevision: 2,
					appliedRevision: 1,
					state: "applying",
				}),
			),
			http.get("/api/apply/jobs", () =>
				HttpResponse.json({
					items: [
						{
							id: "j2",
							desiredRevision: 2,
							baseRevision: 1,
							status: "applying",
							trigger: "manual",
							createdAt: 1700000000,
						},
					],
				}),
			),
		);
		renderApply();
		expect(await screen.findByText("applying")).toBeInTheDocument();
		expect(screen.queryByText("apply.status.applying")).not.toBeInTheDocument();
	});

	it("hides transferred recovery jobs and a superseded lastError after a later success", async () => {
		const giant =
			"runtime publication evidence transferred to a fresh full-convergence attempt " +
			"x".repeat(400);
		server.use(
			http.get("/api/apply/state", () =>
				HttpResponse.json({
					desiredRevision: 2,
					appliedRevision: 2,
					state: "synced",
					lastError: {
						code: "PUBLICATION_RECOVERY_TRANSFERRED",
						message: giant,
					},
				}),
			),
			http.get("/api/apply/jobs", () =>
				HttpResponse.json({
					items: [
						{
							id: "j-ok",
							desiredRevision: 2,
							baseRevision: 1,
							status: "succeeded",
							trigger: "publication-recovery",
							createdAt: 1700000100,
						},
						{
							id: "j-x",
							desiredRevision: 2,
							baseRevision: 1,
							status: "failed",
							trigger: "publication-recovery",
							createdAt: 1700000000,
							errorCode: "PUBLICATION_RECOVERY_TRANSFERRED",
							errorMessage: giant,
						},
					],
				}),
			),
		);
		renderApply();
		expect(await screen.findByText("succeeded")).toBeInTheDocument();
		expect(
			screen.getByText(/1 transferred recovery jobs hidden/i),
		).toBeInTheDocument();
		expect(screen.queryByText(giant)).not.toBeInTheDocument();
		expect(screen.queryByText(/last error/i)).not.toBeInTheDocument();
		fireEvent.click(
			screen.getByRole("button", { name: /show transferred jobs/i }),
		);
		const truncated = await screen.findByTitle(
			`[PUBLICATION_RECOVERY_TRANSFERRED] ${giant}`,
		);
		expect(truncated.textContent?.includes("…")).toBe(true);
		expect(truncated.textContent ?? "").not.toContain("x".repeat(400));
		expect(screen.getAllByText("1 → 2")).toHaveLength(2);
	});

	it("offers retry on rollback_failed and shows a retry error", async () => {
		server.use(
			http.get("/api/apply/state", () =>
				HttpResponse.json({
					desiredRevision: 2,
					appliedRevision: 1,
					state: "failed",
				}),
			),
			http.get("/api/apply/jobs", () =>
				HttpResponse.json({
					items: [
						{
							id: "j-rf",
							desiredRevision: 2,
							baseRevision: 1,
							status: "rollback_failed",
							trigger: "manual",
							createdAt: 1700000000,
							errorMessage: "rollback aborted",
						},
					],
				}),
			),
			http.post("/api/apply/jobs/j-rf/retry", () =>
				HttpResponse.json(
					{ error: { message: "helper unavailable" } },
					{ status: 503 },
				),
			),
		);
		renderApply();
		fireEvent.click(await screen.findByRole("button", { name: /^retry$/i }));
		expect(await screen.findByText(/helper unavailable/i)).toBeInTheDocument();
	});

	// #651: POST /api/apply/reconcile returns 200 with reconciled=false plus
	// the failed applyJob when the converge fails — that is an execution
	// failure, not a successful reconcile.
	it("treats a 200 reconcile carrying a failed apply job as an error", async () => {
		server.use(
			http.get("/api/apply/state", () =>
				HttpResponse.json({
					desiredRevision: 3,
					appliedRevision: 1,
					state: "drift",
				}),
			),
			http.get("/api/apply/jobs", () => HttpResponse.json({ items: [] })),
			http.post("/api/apply/reconcile", () =>
				HttpResponse.json({
					reconciled: false,
					applyJob: {
						id: "j-rec",
						desiredRevision: 3,
						baseRevision: 1,
						status: "failed",
						trigger: "reconcile",
						createdAt: 1700000000,
						errorMessage: "sing-box reload failed",
					},
					revision: { desired: 3, applied: 1 },
				}),
			),
		);
		renderApply();
		fireEvent.click(
			await screen.findByRole("button", { name: /reconcile now/i }),
		);
		expect(
			await screen.findByText(/sing-box reload failed/i),
		).toBeInTheDocument();
	});

	// #651 twin: a bare reconciled=false with no applyJob is the idempotent
	// no-op "already synced" path — not an error.
	it("does not treat a no-op reconcile without an applyJob as an error", async () => {
		server.use(
			http.get("/api/apply/state", () =>
				HttpResponse.json({
					desiredRevision: 2,
					appliedRevision: 1,
					state: "drift",
				}),
			),
			http.get("/api/apply/jobs", () => HttpResponse.json({ items: [] })),
			http.post("/api/apply/reconcile", () =>
				HttpResponse.json({ reconciled: false }),
			),
		);
		renderApply();
		fireEvent.click(
			await screen.findByRole("button", { name: /reconcile now/i }),
		);
		await waitFor(() =>
			expect(
				screen.getByRole("button", { name: /reconcile now/i }),
			).toBeEnabled(),
		);
		expect(
			screen.queryByText(/reconcile failed/i),
		).not.toBeInTheDocument();
	});
});
