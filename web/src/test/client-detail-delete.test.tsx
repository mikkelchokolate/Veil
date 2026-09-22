import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
	createMemoryHistory,
	createRouter,
	RouterProvider,
} from "@tanstack/react-router";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { AuthProvider } from "../auth/AuthContext";
import { I18nProvider } from "../i18n/I18nContext";
import { routeTree } from "../routeTree.gen";
import { HttpResponse, http, server } from "./server";

function renderClientDetail() {
	const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
	const router = createRouter({
		routeTree,
		history: createMemoryHistory({ initialEntries: ["/clients/c1"] }),
	});
	const view = render(
		<QueryClientProvider client={qc}>
			<AuthProvider>
				<I18nProvider>
					<RouterProvider router={router} />
				</I18nProvider>
			</AuthProvider>
		</QueryClientProvider>,
	);
	return { ...view, router };
}

// #653: DELETE /api/v1/clients/{id} returns 200 with success=false when the
// delete committed but the auto-apply failed — the page must surface that
// instead of navigating away as if the delete cleanly converged.
describe("ClientDetailPage delete apply outcome", () => {
	it("stays on the page with an apply-failed badge when the delete commits but apply fails", async () => {
		const user = userEvent.setup();
		let detailGets = 0;
		server.use(
			http.get("/api/inbounds", () => HttpResponse.json([])),
			http.get("/api/v1/clients/c1", () => {
				detailGets += 1;
				return HttpResponse.json({
					id: "c1",
					name: "Alice",
					enabled: true,
					version: 1,
					status: "active",
					bindings: [],
				});
			}),
			http.get("/api/v1/clients", () =>
				HttpResponse.json({ items: [], total: 0, page: 1, pageSize: 25 }),
			),
			http.delete("/api/v1/clients/c1", () =>
				HttpResponse.json({
					id: "c1",
					success: false,
					revision: { desired: 2, applied: 1, state: "failed" },
					applyJob: {
						id: "job-1",
						desiredRevision: 2,
						baseRevision: 1,
						status: "failed",
						trigger: "mutation",
						createdAt: 1700000000,
					},
				}),
			),
		);
		const { router } = renderClientDetail();
		await screen.findByText("Alice");
		await user.click(screen.getByRole("button", { name: /^delete$/i }));
		await user.click(
			await screen.findByRole("button", { name: /confirm delete/i }),
		);
		expect(await screen.findByText(/^apply failed$/i)).toBeInTheDocument();
		expect(router.state.location.pathname).toBe("/clients/c1");
		// The detail query is NOT invalidated — the committed delete would
		// 404 and hide the apply-failed feedback.
		expect(detailGets).toBe(1);
	});

	it("navigates to the list on a clean delete", async () => {
		const user = userEvent.setup();
		server.use(
			http.get("/api/inbounds", () => HttpResponse.json([])),
			http.get("/api/v1/clients/c1", () =>
				HttpResponse.json({
					id: "c1",
					name: "Alice",
					enabled: true,
					version: 1,
					status: "active",
					bindings: [],
				}),
			),
			http.get("/api/v1/clients", () =>
				HttpResponse.json({ items: [], total: 0, page: 1, pageSize: 25 }),
			),
			http.delete("/api/v1/clients/c1", () =>
				HttpResponse.json({
					id: "c1",
					success: true,
					revision: { desired: 2, applied: 2, state: "synced" },
				}),
			),
		);
		const { router } = renderClientDetail();
		await screen.findByText("Alice");
		await user.click(screen.getByRole("button", { name: /^delete$/i }));
		await user.click(
			await screen.findByRole("button", { name: /confirm delete/i }),
		);
		await waitFor(() =>
			expect(router.state.location.pathname).toBe("/clients"),
		);
	});
});
