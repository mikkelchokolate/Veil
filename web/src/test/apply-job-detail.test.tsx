import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
	createMemoryHistory,
	createRouter,
	RouterProvider,
} from "@tanstack/react-router";
import { fireEvent, render, screen } from "@testing-library/react";
import { AuthProvider } from "../auth/AuthContext";
import { I18nProvider } from "../i18n/I18nContext";
import { routeTree } from "../routeTree.gen";
import { HttpResponse, http, server } from "./server";

function renderJob(jobId = "job-1") {
	const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
	const router = createRouter({
		routeTree,
		history: createMemoryHistory({ initialEntries: [`/apply/${jobId}`] }),
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

describe("ApplyJobDetailPage", () => {
	it("offers retry when the job is rollback_failed", async () => {
		server.use(
			http.get("/api/apply/jobs/job-1", () =>
				HttpResponse.json({
					id: "job-1",
					desiredRevision: 2,
					baseRevision: 1,
					status: "rollback_failed",
					trigger: "manual",
					createdAt: 1700000000,
					errorMessage: "rollback aborted",
				}),
			),
			http.get("/api/apply/history", () => HttpResponse.json({ items: [] })),
		);
		renderJob();
		expect(
			await screen.findByRole("button", { name: /retry this revision/i }),
		).toBeInTheDocument();
	});

	it("follows the new apply job after retry instead of claiming queued", async () => {
		server.use(
			http.get("/api/apply/jobs/job-1", () =>
				HttpResponse.json({
					id: "job-1",
					desiredRevision: 2,
					baseRevision: 1,
					status: "failed",
					trigger: "manual",
					createdAt: 1700000000,
					errorMessage: "original failed",
				}),
			),
			http.get("/api/apply/jobs/job-2", () =>
				HttpResponse.json({
					id: "job-2",
					desiredRevision: 2,
					baseRevision: 1,
					status: "failed",
					trigger: "retry",
					createdAt: 1700000001,
					errorMessage: "still broken",
				}),
			),
			http.get("/api/apply/history", () => HttpResponse.json({ items: [] })),
			http.post("/api/apply/jobs/job-1/retry", () =>
				HttpResponse.json({
					applyJob: {
						id: "job-2",
						desiredRevision: 2,
						baseRevision: 1,
						status: "failed",
						trigger: "retry",
						createdAt: 1700000001,
						errorMessage: "still broken",
					},
				}),
			),
		);
		renderJob();
		fireEvent.click(
			await screen.findByRole("button", { name: /retry this revision/i }),
		);
		expect(
			await screen.findByRole("heading", { name: /apply job job-2/i }),
		).toBeInTheDocument();
		expect(
			(await screen.findAllByText(/still broken/i)).length,
		).toBeGreaterThan(0);
		expect(screen.queryByText(/retry queued/i)).not.toBeInTheDocument();
	});

	it("opens the succeeded retry job rather than the original failure", async () => {
		server.use(
			http.get("/api/apply/jobs/job-1", () =>
				HttpResponse.json({
					id: "job-1",
					desiredRevision: 2,
					baseRevision: 1,
					status: "failed",
					trigger: "manual",
					createdAt: 1700000000,
					errorMessage: "original failed",
				}),
			),
			http.get("/api/apply/jobs/job-2", () =>
				HttpResponse.json({
					id: "job-2",
					desiredRevision: 2,
					baseRevision: 1,
					status: "succeeded",
					trigger: "retry",
					createdAt: 1700000001,
				}),
			),
			http.get("/api/apply/history", () => HttpResponse.json({ items: [] })),
			http.post("/api/apply/jobs/job-1/retry", () =>
				HttpResponse.json({
					applyJob: {
						id: "job-2",
						desiredRevision: 2,
						baseRevision: 1,
						status: "succeeded",
						trigger: "retry",
						createdAt: 1700000001,
					},
				}),
			),
		);
		renderJob();
		fireEvent.click(
			await screen.findByRole("button", { name: /retry this revision/i }),
		);
		expect(
			await screen.findByRole("heading", { name: /apply job job-2/i }),
		).toBeInTheDocument();
		expect(screen.queryByText(/retry queued/i)).not.toBeInTheDocument();
		expect(screen.queryByText(/original failed/i)).not.toBeInTheDocument();
	});

	// #675: a 200 retry body with success=false is an explicit execution
	// failure (#544) — show the error and stay on the failed job instead of
	// navigating to the retry job or claiming it was queued.
	it("shows a retry error and stays put when the 200 body reports success:false", async () => {
		server.use(
			http.get("/api/apply/jobs/job-1", () =>
				HttpResponse.json({
					id: "job-1",
					desiredRevision: 2,
					baseRevision: 1,
					status: "failed",
					trigger: "manual",
					createdAt: 1700000000,
					errorMessage: "original failed",
				}),
			),
			http.get("/api/apply/jobs/job-2", () =>
				HttpResponse.json({
					id: "job-2",
					desiredRevision: 2,
					baseRevision: 1,
					status: "failed",
					trigger: "retry",
					createdAt: 1700000001,
					errorMessage: "still broken",
				}),
			),
			http.get("/api/apply/history", () => HttpResponse.json({ items: [] })),
			http.post("/api/apply/jobs/job-1/retry", () =>
				HttpResponse.json({
					success: false,
					error: "retry apply did not converge",
					applyJob: {
						id: "job-2",
						desiredRevision: 2,
						baseRevision: 1,
						status: "failed",
						trigger: "retry",
						createdAt: 1700000001,
					},
				}),
			),
		);
		renderJob();
		fireEvent.click(
			await screen.findByRole("button", { name: /retry this revision/i }),
		);
		expect(
			await screen.findByText(/retry apply did not converge/i),
		).toBeInTheDocument();
		expect(
			screen.getByRole("heading", { name: /apply job job-1/i }),
		).toBeInTheDocument();
		expect(screen.queryByText(/retry queued/i)).not.toBeInTheDocument();
	});

	it("renders a 422 plan body instead of a load error", async () => {
		server.use(
			http.get("/api/apply/jobs/job-1", () =>
				HttpResponse.json({
					id: "job-1",
					desiredRevision: 2,
					baseRevision: 1,
					status: "failed",
					trigger: "manual",
					createdAt: 1700000000,
				}),
			),
			http.get("/api/apply/history", () => HttpResponse.json({ items: [] })),
			http.post("/api/apply/plan", () =>
				HttpResponse.json(
					{
						valid: false,
						configs: [{ name: "haproxy.cfg", content: "broken" }],
						operations: [],
					},
					{ status: 422 },
				),
			),
		);
		renderJob();
		fireEvent.click(await screen.findByRole("button", { name: /show plan/i }));
		expect(await screen.findByText(/1 config\(s\)/i)).toBeInTheDocument();
		expect(screen.queryByText(/failed to load plan/i)).not.toBeInTheDocument();
	});

	it("does not treat a failed history fetch as empty", async () => {
		server.use(
			http.get("/api/apply/jobs/job-1", () =>
				HttpResponse.json({
					id: "job-1",
					desiredRevision: 1,
					baseRevision: 1,
					status: "succeeded",
					trigger: "manual",
					createdAt: 1700000000,
				}),
			),
			http.get("/api/apply/history", () =>
				HttpResponse.json(
					{ error: { message: "history down" } },
					{ status: 500 },
				),
			),
		);
		renderJob();
		expect(
			await screen.findByText(/failed to load apply history/i),
		).toBeInTheDocument();
		expect(screen.queryByText(/no history entries/i)).not.toBeInTheDocument();
	});
});
