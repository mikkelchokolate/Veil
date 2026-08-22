import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
	createMemoryHistory,
	createRouter,
	RouterProvider,
} from "@tanstack/react-router";
import { render, screen, waitFor } from "@testing-library/react";
import { AuthProvider } from "../auth/AuthContext";
import { I18nProvider } from "../i18n/I18nContext";
import { routeTree } from "../routeTree.gen";
import { HttpResponse, http, server } from "./server";

// Render the app inside its REAL generated route tree (blocker W6: the page
// reads typed search via Route.useSearch() from the /clients/ file route with
// its Zod validateSearch — file routes are pre-linked to the app root, so a
// hand-built tree would duplicate __root__) + react-query, against the MSW
// mock API.
function renderClients(initialPath = "/clients") {
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

describe("ClientsPage", () => {
	it("renders clients from the API", async () => {
		server.use(
			http.get("/api/v1/clients", () =>
				HttpResponse.json({
					items: [
						{
							id: "c1",
							name: "Alice",
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
		renderClients();
		await waitFor(() => expect(screen.getByText("Alice")).toBeInTheDocument());
	});

	it("maps the depleted status filter to quotaState", async () => {
		let seen = "";
		server.use(
			http.get("/api/v1/clients", ({ request }) => {
				seen = new URL(request.url).search;
				return HttpResponse.json({
					items: [],
					total: 0,
					page: 1,
					pageSize: 20,
				});
			}),
		);
		renderClients("/clients?status=depleted");
		await waitFor(() => expect(seen).toContain("quotaState=depleted"));
		expect(seen).not.toContain("status=depleted");
	});

	it("shows empty state when no clients", async () => {
		server.use(
			http.get("/api/v1/clients", () =>
				HttpResponse.json({ items: [], total: 0, page: 1, pageSize: 20 }),
			),
		);
		renderClients();
		await waitFor(() =>
			expect(screen.getByText(/no clients/i)).toBeInTheDocument(),
		);
	});
});
