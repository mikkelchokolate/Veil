import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
	createMemoryHistory,
	createRouter,
	RouterProvider,
} from "@tanstack/react-router";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { vi } from "vitest";
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

	// #715/#718: neither the live plan preview nor the global apply history
	// is scoped to this job — the UI must say so, and the copy report must not
	// embed global entries as this job's history.
	it("labels live plan + global history and keeps the report job-scoped", async () => {
		const writeText = vi.fn().mockResolvedValue(undefined);
		Object.defineProperty(navigator, "clipboard", {
			value: { writeText },
			configurable: true,
		});
		server.use(
			http.get("/api/apply/jobs/job-1", () =>
				HttpResponse.json({
					id: "job-1",
					desiredRevision: 2,
					baseRevision: 1,
					status: "failed",
					trigger: "manual",
					createdAt: 1700000000,
					errorMessage: "boom",
				}),
			),
			http.get("/api/apply/history", () =>
				HttpResponse.json({
					items: [
						{
							id: "h-9",
							timestamp: "2023-11-14T00:00:00Z",
							stage: "apply",
							success: false,
							applied: false,
							liveApplied: false,
							servicesApplied: false,
						},
					],
				}),
			),
		);
		renderJob();
		expect(
			await screen.findByText(/current plan preview \(live\)/i),
		).toBeInTheDocument();
		expect(screen.getByText(/not a snapshot of this job/i)).toBeInTheDocument();
		expect(
			await screen.findByText(/global apply history \(all jobs\)/i),
		).toBeInTheDocument();
		fireEvent.click(screen.getByRole("button", { name: /copy report/i }));
		await waitFor(() => expect(writeText).toHaveBeenCalled());
		const report = String(writeText.mock.calls[0]?.[0]);
		expect(report).toContain("id: job-1");
		expect(report).not.toContain("h-9");
		expect(report).not.toContain("history:");
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
