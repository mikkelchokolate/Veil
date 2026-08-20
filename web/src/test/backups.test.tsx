import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { I18nProvider } from "../i18n/I18nContext";
import { BackupsPage } from "../pages/BackupsPage";

const fetcherMocks = vi.hoisted(() => ({
	apiFetch: vi.fn(),
	apiUrl: vi.fn((path: string) => `/hidden-panel${path}`),
}));

vi.mock("../api/fetcher", () => ({
	ApiError: class ApiError extends Error {},
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

	it("shows dismiss after a succeeded restore job", async () => {
		fetcherMocks.apiFetch.mockImplementation((path: string, init?: RequestInit) => {
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
		fireEvent.click(
			await screen.findByRole("button", { name: "Confirm restore" }),
		);

		expect(await screen.findByText("succeeded")).toBeInTheDocument();
		expect(screen.getByRole("button", { name: "Dismiss" })).toBeInTheDocument();
	});
});
