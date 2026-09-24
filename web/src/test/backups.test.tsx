import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
	fireEvent,
	render,
	screen,
	waitFor,
	within,
} from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { ApiError } from "../api/fetcher";
import { I18nProvider } from "../i18n/I18nContext";
import { BackupsPage } from "../pages/BackupsPage";

const fetcherMocks = vi.hoisted(() => ({
	apiFetch: vi.fn(),
	apiUrl: vi.fn((path: string) => `/hidden-panel${path}`),
	notifyUnauthorized: vi.fn(),
	mutationErrorMessage: vi.fn((e: unknown, fallback: string) =>
		e instanceof Error ? e.message : fallback,
	),
	// #848: viewer coverage needs the role switchable per test.
	isAdmin: { value: true },
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
	mutationErrorMessage: fetcherMocks.mutationErrorMessage,
	notifyUnauthorized: fetcherMocks.notifyUnauthorized,
}));

vi.mock("../auth/AuthContext", () => ({
	useIsAdmin: () => fetcherMocks.isAdmin.value,
}));

afterEach(() => {
	vi.restoreAllMocks();
	vi.unstubAllGlobals();
	fetcherMocks.apiFetch.mockReset();
	fetcherMocks.apiUrl.mockClear();
	fetcherMocks.notifyUnauthorized.mockReset();
	fetcherMocks.isAdmin.value = true;
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

	// #848: the whole backups surface is admin-only — a viewer must see the
	// adminRequired notice, not the create/restore/delete controls.
	it("shows adminRequired to viewers instead of the backup controls", async () => {
		fetcherMocks.isAdmin.value = false;
		fetcherMocks.apiFetch.mockResolvedValue({ items: [] });
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
			await screen.findByText(/require the admin role/i),
		).toBeInTheDocument();
		expect(
			screen.queryByRole("button", { name: /create backup/i }),
		).not.toBeInTheDocument();
		expect(
			screen.queryByRole("button", { name: /prune old backups/i }),
		).not.toBeInTheDocument();
	});

	// #757: archive Delete was already gated — lock no-request-until-confirm
	// so a one-click regression fails this suite like the routing twin.
	it("does not DELETE an archive until delete is confirmed", async () => {
		const deletes: string[] = [];
		fetcherMocks.apiFetch.mockImplementation(
			(path: string, init?: RequestInit) => {
				if (path === "/api/backups") {
					return Promise.resolve({
						items: [
							{
								name: "veil-backup.enc",
								size: 42,
								createdAt: "2026-08-17T03:39:09Z",
								encrypted: true,
							},
						],
					});
				}
				if (
					path === "/api/backups/veil-backup.enc" &&
					init?.method === "DELETE"
				) {
					deletes.push(path);
					return Promise.resolve({});
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
		fireEvent.click(await screen.findByRole("button", { name: /^delete$/i }));
		const dialog = await screen.findByRole("alertdialog");
		expect(deletes).toEqual([]);
		// The dialog action shares the row label — click it inside the dialog.
		fireEvent.click(within(dialog).getByRole("button", { name: /^delete$/i }));
		await waitFor(() =>
			expect(deletes).toEqual(["/api/backups/veil-backup.enc"]),
		);
	});

	// #813: restore is gated behind its AlertDialog — the POST must not fire
	// on the row button, only on the dialog action (same lock as prune).
	it("does not POST a restore until restore is confirmed", async () => {
		const restoreBodies: unknown[] = [];
		fetcherMocks.apiFetch.mockImplementation(
			(path: string, init?: RequestInit) => {
				if (path === "/api/backups") {
					return Promise.resolve({
						items: [
							{
								name: "veil-backup.enc",
								size: 42,
								createdAt: "2026-08-17T03:39:09Z",
								encrypted: true,
							},
						],
					});
				}
				if (
					path === "/api/backups/veil-backup.enc/restore" &&
					init?.method === "POST"
				) {
					restoreBodies.push(
						typeof init.body === "string" ? JSON.parse(init.body) : init.body,
					);
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
		expect(await screen.findByRole("alertdialog")).toBeInTheDocument();
		expect(restoreBodies).toEqual([]);
		fireEvent.click(screen.getByRole("button", { name: "Confirm restore" }));
		await waitFor(() => expect(restoreBodies).toHaveLength(1));
		expect(restoreBodies[0]).toEqual({ confirm: true });
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

	// #696: a session-ending 401 on the raw fetch download must still reach
	// the shared unauthorized handler instead of only painting an error.
	it("routes a 401 download through the unauthorized handler", async () => {
		fetcherMocks.apiFetch.mockResolvedValue({
			items: [
				{
					name: "veil.enc",
					size: 42,
					createdAt: "2026-08-17T03:39:09Z",
					encrypted: true,
				},
			],
		});
		vi.stubGlobal(
			"fetch",
			vi.fn().mockResolvedValue(new Response("no", { status: 401 })),
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

		fireEvent.click(await screen.findByRole("button", { name: "Download" }));

		await waitFor(() =>
			expect(fetcherMocks.notifyUnauthorized).toHaveBeenCalledWith(
				"/api/backups/veil.enc/download",
				401,
			),
		);
		expect(
			await screen.findByText(/download failed: 401/i),
		).toBeInTheDocument();
	});

	// #697: a live create response can carry a warning (e.g. prune failure)
	// — the notice must not read as a clean create.
	it("surfaces a warning from a successful create", async () => {
		fetcherMocks.apiFetch.mockImplementation(
			(path: string, init?: RequestInit) => {
				if (path === "/api/backups" && init?.method === "POST") {
					return Promise.resolve({
						archive: { name: "veil-1.enc" },
						warning: "retention prune failed",
					});
				}
				if (path === "/api/backups") {
					return Promise.resolve({ items: [] });
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
			await screen.findByRole("button", { name: "Create backup" }),
		);

		expect(
			await screen.findByText(/warning: retention prune failed/i),
		).toBeInTheDocument();
		expect(screen.queryByText(/^backup created\.$/i)).not.toBeInTheDocument();
	});

	// #698: a failed verify must stamp the row — not leave it at "—" as if
	// the archive was never checked.
	it("stamps the row when verify fails", async () => {
		fetcherMocks.apiFetch.mockImplementation(
			(path: string, init?: RequestInit) => {
				if (path === "/api/backups") {
					return Promise.resolve({
						items: [
							{
								name: "bad.enc",
								size: 42,
								createdAt: "2026-08-17T03:39:09Z",
								encrypted: true,
							},
						],
					});
				}
				if (path === "/api/backups/bad.enc/verify" && init?.method === "POST") {
					return Promise.reject(new ApiError(500, "corrupt archive"));
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

		fireEvent.click(await screen.findByRole("button", { name: "Verify" }));

		// The row is stamped with the failure, not left at "—".
		expect(await screen.findAllByText(/corrupt archive/i)).not.toHaveLength(0);
		const row = screen.getByText("bad.enc").closest("tr");
		expect(row).not.toBeNull();
		// Assert the stamped error inside the row itself — a blanket em-dash
		// check on the whole row is brittle against unrelated cells.
		if (row) {
			expect(within(row).getByText(/corrupt archive/i)).toBeInTheDocument();
		}
	});

	// #722: the prune notice must reflect the real deleted count — a no-op
	// prune and a mass delete cannot read the same.
	it("reports the deleted count from prune", async () => {
		fetcherMocks.apiFetch.mockImplementation(
			(path: string, init?: RequestInit) => {
				if (path === "/api/backups") {
					return Promise.resolve({ items: [] });
				}
				if (path === "/api/backups/prune" && init?.method === "POST") {
					return Promise.resolve({
						deleted: ["a.enc", "b.enc"],
						kept: ["c.enc"],
						dryRun: false,
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

		fireEvent.click(
			await screen.findByRole("button", { name: "Prune old backups" }),
		);
		fireEvent.click(
			await screen.findByRole("button", { name: /^confirm prune$/i }),
		);

		expect(await screen.findByText(/pruned 2 old backup/i)).toBeInTheDocument();
	});

	it("does not claim deletion when prune removed nothing", async () => {
		fetcherMocks.apiFetch.mockImplementation(
			(path: string, init?: RequestInit) => {
				if (path === "/api/backups") {
					return Promise.resolve({ items: [] });
				}
				if (path === "/api/backups/prune" && init?.method === "POST") {
					return Promise.resolve({
						deleted: [],
						kept: ["c.enc"],
						dryRun: false,
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

		fireEvent.click(
			await screen.findByRole("button", { name: "Prune old backups" }),
		);
		fireEvent.click(
			await screen.findByRole("button", { name: /^confirm prune$/i }),
		);

		expect(await screen.findByText(/nothing was deleted/i)).toBeInTheDocument();
		expect(screen.queryByText(/old backups pruned/i)).not.toBeInTheDocument();
	});

	// #720: a pending (key publication) job means the archive was NOT
	// restored — the card must say so instead of looking like a finished job.
	it("explains a pending key-publication restore instead of a plain pending badge", async () => {
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
						status: "pending",
						outcome: "pending_key_publication",
						phase: "key_publication_pending",
						restored: false,
						httpStatus: 202,
						safetyKeyPath: "/var/lib/veil/safety.key",
					});
				}
				if (path === "/api/backup-restore-jobs/job-1") {
					return Promise.resolve({
						id: "job-1",
						archive: "veil-backup.enc",
						status: "pending",
						outcome: "pending_key_publication",
						phase: "key_publication_pending",
						restored: false,
						httpStatus: 202,
						safetyKeyPath: "/var/lib/veil/safety.key",
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

		expect(
			await screen.findByText(/awaiting key publication/i),
		).toBeInTheDocument();
		expect(
			await screen.findByText(
				/publishing the new state key is still required/i,
			),
		).toBeInTheDocument();
		expect(
			await screen.findByText(/\/var\/lib\/veil\/safety\.key/),
		).toBeInTheDocument();
		// The dismiss affordance must not read like a finished-job dismissal.
		expect(
			screen.getByRole("button", {
				name: /dismiss \(archive was not restored\)/i,
			}),
		).toBeInTheDocument();
		expect(
			screen.queryByRole("button", { name: /^dismiss$/i }),
		).not.toBeInTheDocument();
	});

	// #728: the restore confirm must state that a successful restore revokes
	// every other panel session — not just hedge "you may be logged out".
	it("warns that restore revokes every other panel session", async () => {
		fetcherMocks.apiFetch.mockResolvedValue({
			items: [
				{
					name: "veil-backup.enc",
					size: 42,
					createdAt: "2026-08-17T03:39:09Z",
					encrypted: true,
				},
			],
		});

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

		expect(
			await screen.findByText(/revokes every other panel session/i),
		).toBeInTheDocument();
	});
});
