import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
	createMemoryHistory,
	createRouter,
	RouterProvider,
} from "@tanstack/react-router";
import { render, screen, waitFor } from "@testing-library/react";
import { afterAll, beforeAll, describe, expect, it } from "vitest";
import { AuthProvider } from "../auth/AuthContext";
import { I18nProvider } from "../i18n/I18nContext";
import { routeTree } from "../routeTree.gen";
import { worker } from "./browserWorker";
import { HttpResponse, http } from "./handlers";

// Real-browser (Chromium) suite — blocker W8. Same app + route tree + MSW
// mock API as the jsdom suite, but executed in an actual browser engine via
// the MSW Service Worker, catching jsdom-only blind spots (real fetch, real
// DOM, real event loop).
beforeAll(() => worker.start({ onUnhandledRequest: "error", quiet: true }));
afterAll(() => worker.stop());

function renderApp(initialPath = "/clients") {
	const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
	const router = createRouter({
		routeTree,
		history: createMemoryHistory({ initialEntries: [initialPath] }),
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

describe("ClientsPage (real browser)", () => {
	it("renders clients from the API in Chromium", async () => {
		worker.use(
			http.get("/api/v1/clients", () =>
				HttpResponse.json({
					items: [
						{
							id: "c1",
							name: "browser-client",
							status: "active",
							enabled: true,
							createdAt: 1700000000,
							bindingCount: 1,
						},
					],
					total: 1,
					page: 1,
					pageSize: 20,
				}),
			),
		);
		renderApp("/clients");
		await waitFor(() =>
			expect(screen.getByText("browser-client")).toBeInTheDocument(),
		);
	});

	it("shows empty state when no clients", async () => {
		worker.use(
			http.get("/api/v1/clients", () =>
				HttpResponse.json({ items: [], total: 0, page: 1, pageSize: 20 }),
			),
		);
		renderApp("/clients");
		await waitFor(() =>
			expect(screen.getByText(/no clients/i)).toBeInTheDocument(),
		);
	});
});
