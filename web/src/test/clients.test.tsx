import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
	createMemoryHistory,
	createRootRoute,
	createRoute,
	createRouter,
	RouterProvider,
} from "@tanstack/react-router";
import { render, screen, waitFor } from "@testing-library/react";
import { AuthProvider } from "../auth/AuthContext";
import { ClientsPage } from "../pages/ClientsPage";
import { HttpResponse, http, server } from "./server";

// Render a single page component inside a minimal router (clients list uses
// useSearch/useNavigate) + react-query, against the MSW mock API.
function renderClients(initialPath = "/clients") {
	const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
	const root = createRootRoute();
	const route = createRoute({
		getParentRoute: () => root,
		path: "/clients",
		component: ClientsPage,
		validateSearch: (s: Record<string, unknown>) => s,
	});
	const router = createRouter({
		routeTree: root.addChildren([route]),
		history: createMemoryHistory({ initialEntries: [initialPath] }),
	});
	return render(
		<QueryClientProvider client={qc}>
			<AuthProvider>
				<RouterProvider router={router} />
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
