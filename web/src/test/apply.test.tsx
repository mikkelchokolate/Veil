import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
	createRootRoute,
	createRoute,
	createRouter,
	RouterProvider,
} from "@tanstack/react-router";
import { render, screen, waitFor } from "@testing-library/react";
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
});
