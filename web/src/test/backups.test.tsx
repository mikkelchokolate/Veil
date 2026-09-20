import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { ApiError } from "../api/fetcher";
import { I18nProvider } from "../i18n/I18nContext";
import { BackupsPage } from "../pages/BackupsPage";

const fetcherMocks = vi.hoisted(() => ({
	apiFetch: vi.fn(),
	apiUrl: vi.fn((path: string) => `/hidden-panel${path}`),
}));

vi.mock("../api/fetcher", () => ({
	ApiError: class ApiError extends Error {
		status: number;
		body: unknown;
		constructor(status: number, message: string) {
			super(message);
			this.status = status;
		}
	},
	apiFetch: fetcherMocks.apiFetch,
	apiUrl: fetcherMocks.apiUrl,
}));

vi.mock("../auth/AuthContext", () => ({
	useIsAdmin: () => true,
}));

afterEach(() => {
	vi.restoreAllMocks();
	vi.unstubAllGlobals();
	fetcherMocks.apiFetch.mockReset();
	fetcherMocks.apiUrl.mockClear();
});

describe("BackupsPage", () => {
	it("downloads through the configured panel base path", async () => {
		fetcherMocks.apiFetch.mockResolvedValue({
			items: [
				{
					name: "veil backup.enc",
					size: 42,
					createdAt: "2026-08-17T03:39:09Z",
					encrypted: true,
				},
			],
		});
		const fetchMock = vi.fn().mockResolvedValue(new Response("archive"));
		vi.stubGlobal("fetch", fetchMock);
		vi.stubGlobal(
			"URL",
			class extends URL {
				static createObjectURL() {
					return "blob:backup";
				}

				static revokeObjectURL() {}
			},
		);
		vi.spyOn(HTMLAnchorElement.prototype, "click").mockImplementation(() => {});

		const queryClient = new QueryClient({
			defaultOptions: { queries: { retry: false } },
		});
		render(
			<QueryClientProvider client={queryClient}>
				<I18nProvider>
					<BackupsPage />
				</I18nProvider>
			</QueryClientProvider>,
		);

		fireEvent.click(await screen.findByRole("button", { name: "Download" }));

		await waitFor(() =>
			expect(fetchMock).toHaveBeenCalledWith(
				"/hidden-panel/api/backups/veil%20backup.enc/download",
				{ credentials: "same-origin" },
			),
		);
		expect(fetcherMocks.apiUrl).toHaveBeenCalledWith(
			"/api/backups/veil%20backup.enc/download",
		);
	});

	it("confirms prune and posts the displayed 7/4/12 retention", async () => {
		const pruneBodies: unknown[] = [];
		fetcherMocks.apiFetch.mockImplementation(
			(path: string, init?: RequestInit) => {
				if (path === "/api/backups") {
					return Promise.resolve({ items: [] });
				}
				if (path === "/api/backups/prune" && init?.method === "POST") {
					pruneBodies.push(
						typeof init.body === "string" ? JSON.parse(init.body) : init.body,
					);
					return Promise.resolve({ pruned: 1 });
				}
				return Promise.resolve({});
			},
		);
		const queryClient = new QueryClient({
			defaultOptions: { queries: { retry: false } },
		});
		render(
			<QueryClientProvider client={queryClient}>
				<I18nProvider>
					<BackupsPage />
				</I18nProvider>
			</QueryClientProvider>,
		);
		fireEvent.click(
			await screen.findByRole("button", { name: "Prune old backups" }),
		);
		expect(pruneBodies).toEqual([]);
		expect(
			await screen.findByText(/keep 7 daily, 4 weekly, and 12 monthly/i),
		).toBeInTheDocument();
		fireEvent.click(screen.getByRole("button", { name: /^confirm prune$/i }));
		await waitFor(() => expect(pruneBodies).toHaveLength(1));
		expect(pruneBodies[0]).toEqual({ daily: 7, weekly: 4, monthly: 12 });
	});

	it("shows dismiss after a succeeded restore job", async () => {
		fetcherMocks.apiFetch.mockImplementation(
			(path: string, init?: RequestInit) => {
				if (path === "/api/backups") {
					return Promise.resolve([
						{
							name: "veil-backup.enc",
							size: 42,
							createdAt: "2026-08-17T03:39:09Z",
							encrypted: true,
						},
					]);
				}
				if (
					path === "/api/backups/veil-backup.enc/restore" &&
					init?.method === "POST"
				) {
					return Promise.resolve({
						id: "job-1",
						archive: "veil-backup.enc",
						status: "succeeded",
					});
				}
				return Promise.resolve({});
			},
		);

		const queryClient = new QueryClient({
			defaultOptions: { queries: { retry: false } },
		});
		render(
			<QueryClientProvider client={queryClient}>
				<I18nProvider>
					<BackupsPage />
				</I18nProvider>
			</QueryClientProvider>,
		);

		fireEvent.click(await screen.findByRole("button", { name: "Restore" }));
		fireEvent.click(
			await screen.findByRole("button", { name: "Confirm restore" }),
		);

		expect(await screen.findByText("succeeded")).toBeInTheDocument();
		expect(screen.getByRole("button", { name: "Dismiss" })).toBeInTheDocument();
	});

	it("does not treat a failed backup list as empty", async () => {
		fetcherMocks.apiFetch.mockRejectedValue(new Error("down"));
		const queryClient = new QueryClient({
			defaultOptions: { queries: { retry: false } },
		});
		render(
			<QueryClientProvider client={queryClient}>
				<I18nProvider>
					<BackupsPage />
				</I18nProvider>
			</QueryClientProvider>,
		);
		expect(
			await screen.findByText(/failed to load backups/i),
		).toBeInTheDocument();
		expect(screen.queryByText(/no backups yet/i)).not.toBeInTheDocument();
	});

	it("does not leave a restore job looking queued when status polling fails", async () => {
		fetcherMocks.apiFetch.mockImplementation(
			(path: string, init?: RequestInit) => {
				if (path === "/api/backups") {
					return Promise.resolve([
						{
							name: "veil-backup.enc",
							size: 42,
							createdAt: "2026-08-17T03:39:09Z",
							encrypted: true,
						},
					]);
				}
				if (
					path === "/api/backups/veil-backup.enc/restore" &&
					init?.method === "POST"
				) {
					return Promise.resolve({
						id: "job-1",
						archive: "veil-backup.enc",
						status: "queued",
					});
				}
				if (path === "/api/backup-restore-jobs/job-1") {
					return Promise.reject(new Error("job down"));
				}
				return Promise.resolve({});
			},
		);

		const queryClient = new QueryClient({
			defaultOptions: { queries: { retry: false } },
		});
		render(
			<QueryClientProvider client={queryClient}>
				<I18nProvider>
					<BackupsPage />
				</I18nProvider>
			</QueryClientProvider>,
		);

		fireEvent.click(await screen.findByRole("button", { name: "Restore" }));
		fireEvent.click(
			await screen.findByRole("button", { name: "Confirm restore" }),
		);

		expect(
			await screen.findByText(/failed to load restore job status/i),
		).toBeInTheDocument();
	});

	it("surfaces a failed restore job when the poll returns the job as HTTP 500", async () => {
		fetcherMocks.apiFetch.mockImplementation(
			(path: string, init?: RequestInit) => {
				if (path === "/api/backups") {
					return Promise.resolve([
						{
							name: "veil-backup.enc",
							size: 42,
							createdAt: "2026-08-17T03:39:09Z",
							encrypted: true,
						},
					]);
				}
				if (
					path === "/api/backups/veil-backup.enc/restore" &&
					init?.method === "POST"
				) {
					return Promise.resolve({
						id: "job-1",
						archive: "veil-backup.enc",
						status: "queued",
					});
				}
				if (path === "/api/backup-restore-jobs/job-1") {
					const err = new ApiError(500, "disk full");
					err.body = {
						id: "job-1",
						archive: "veil-backup.enc",
						status: "failed",
						error: "disk full",
					};
					return Promise.reject(err);
				}
				return Promise.resolve({});
			},
		);

		const queryClient = new QueryClient({
			defaultOptions: { queries: { retry: false } },
		});
		render(
			<QueryClientProvider client={queryClient}>
				<I18nProvider>
					<BackupsPage />
				</I18nProvider>
			</QueryClientProvider>,
		);

		fireEvent.click(await screen.findByRole("button", { name: "Restore" }));
		fireEvent.click(
			await screen.findByRole("button", { name: "Confirm restore" }),
		);

		expect(await screen.findByText("failed")).toBeInTheDocument();
		expect(await screen.findByText("disk full")).toBeInTheDocument();
		expect(screen.getByRole("button", { name: "Dismiss" })).toBeInTheDocument();
		expect(
			screen.queryByText(/failed to load restore job status/i),
		).not.toBeInTheDocument();
	});

	it("dismisses a degraded restore job returned as HTTP 500 JSON", async () => {
		fetcherMocks.apiFetch.mockImplementation(
			(path: string, init?: RequestInit) => {
				if (path === "/api/backups") {
					return Promise.resolve([
						{
							name: "veil-backup.enc",
							size: 42,
							createdAt: "2026-08-17T03:39:09Z",
							encrypted: true,
						},
					]);
				}
				if (
					path === "/api/backups/veil-backup.enc/restore" &&
					init?.method === "POST"
				) {
					return Promise.resolve({
						id: "job-1",
						archive: "veil-backup.enc",
						status: "queued",
					});
				}
				if (path === "/api/backup-restore-jobs/job-1") {
					const err = new ApiError(500, "revalidation failed");
					err.body = {
						id: "job-1",
						archive: "veil-backup.enc",
						status: "degraded",
						error: "revalidation failed",
					};
					return Promise.reject(err);
				}
				return Promise.resolve({});
			},
		);

		const queryClient = new QueryClient({
			defaultOptions: { queries: { retry: false } },
		});
		render(
			<QueryClientProvider client={queryClient}>
				<I18nProvider>
					<BackupsPage />
				</I18nProvider>
			</QueryClientProvider>,
		);

		fireEvent.click(await screen.findByRole("button", { name: "Restore" }));
		fireEvent.click(
			await screen.findByRole("button", { name: "Confirm restore" }),
		);

		expect(await screen.findByText(/^degraded$/i)).toBeInTheDocument();
		expect(
			screen.queryByText("backups.status.degraded"),
		).not.toBeInTheDocument();
		expect(await screen.findByText("revalidation failed")).toBeInTheDocument();
		expect(screen.getByRole("button", { name: "Dismiss" })).toBeInTheDocument();
	});

	// #586: a degraded job with restored=true committed state — it must
	// surface as a warning success with the outcome/phase visible, not a
	// hard failure that invites a second restore.
	it("treats a degraded-but-restored job as a warning, not a failure", async () => {
		fetcherMocks.apiFetch.mockImplementation(
			(path: string, init?: RequestInit) => {
				if (path === "/api/backups") {
					return Promise.resolve([
						{
							name: "veil-backup.enc",
							size: 42,
							createdAt: "2026-08-17T03:39:09Z",
							encrypted: true,
						},
					]);
				}
				if (
					path === "/api/backups/veil-backup.enc/restore" &&
					init?.method === "POST"
				) {
					return Promise.resolve({
						id: "job-1",
						archive: "veil-backup.enc",
						status: "queued",
					});
				}
				if (path === "/api/backup-restore-jobs/job-1") {
					return Promise.resolve({
						id: "job-1",
						archive: "veil-backup.enc",
						status: "degraded",
						outcome: "restored",
						phase: "finalization_failed",
						restored: true,
						httpStatus: 500,
						error: "finalization failed",
					});
				}
				return Promise.resolve({});
			},
		);

		const queryClient = new QueryClient({
			defaultOptions: { queries: { retry: false } },
		});
		render(
			<QueryClientProvider client={queryClient}>
				<I18nProvider>
					<BackupsPage />
				</I18nProvider>
			</QueryClientProvider>,
		);

		fireEvent.click(await screen.findByRole("button", { name: "Restore" }));
		fireEvent.click(
			await screen.findByRole("button", { name: "Confirm restore" }),
		);

		const badge = await screen.findByText(/^degraded$/i);
		expect(badge.className).toContain("--warning");
		expect(badge.className).not.toContain("--danger");
		expect(await screen.findByText(/outcome: restored/i)).toBeInTheDocument();
		expect(
			await screen.findByText(/phase: finalization failed/i),
		).toBeInTheDocument();
		expect(
			await screen.findByText(/restored state is committed/i),
		).toBeInTheDocument();
		expect(screen.getByRole("button", { name: "Dismiss" })).toBeInTheDocument();
	});
});
