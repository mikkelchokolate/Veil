import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
	createMemoryHistory,
	createRouter,
	RouterProvider,
} from "@tanstack/react-router";
import { render, screen } from "@testing-library/react";
import { AuthProvider } from "../auth/AuthContext";
import { I18nProvider } from "../i18n/I18nContext";
import { routeTree } from "../routeTree.gen";
import { HttpResponse, http, server } from "./server";

function renderNewClient() {
	const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
	const router = createRouter({
		routeTree,
		history: createMemoryHistory({ initialEntries: ["/clients/new"] }),
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

describe("ClientNewPage RBAC", () => {
	it("does not expose the create wizard to a viewer", async () => {
		server.use(
			http.get("/api/auth/status", () =>
				HttpResponse.json({
					authenticated: true,
					username: "look",
					role: "viewer",
					csrfToken: "csrf",
				}),
			),
			http.get("/api/inbounds", () => HttpResponse.json([])),
			http.get("/api/v1/clients", () =>
				HttpResponse.json({ items: [], total: 0, page: 1, pageSize: 25 }),
			),
		);
		renderNewClient();
		expect(
			await screen.findByText(/creating clients requires the admin role/i),
		).toBeInTheDocument();
		expect(screen.queryByLabelText(/^name$/i)).not.toBeInTheDocument();
		expect(
			screen.queryByRole("button", { name: /create client/i }),
		).not.toBeInTheDocument();
	});
});
