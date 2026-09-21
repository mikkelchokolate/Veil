import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
	createMemoryHistory,
	createRouter,
	RouterProvider,
} from "@tanstack/react-router";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
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

// #646: POST /api/v1/clients returns 201 with success=false when the client
// committed but the auto-apply failed — the wizard must not paint that as a
// clean "Client created".
describe("ClientNewPage apply-failed outcome", () => {
	it("shows a warning instead of the green created badge when apply fails", async () => {
		const user = userEvent.setup();
		server.use(
			http.get("/api/inbounds", () => HttpResponse.json([])),
			http.get("/api/v1/clients", () =>
				HttpResponse.json({ items: [], total: 0, page: 1, pageSize: 25 }),
			),
			http.post("/api/v1/clients", () =>
				HttpResponse.json(
					{
						client: {
							id: "c1",
							name: "alice",
							enabled: true,
							version: 1,
							status: "active",
						},
						success: false,
						revision: { desired: 2, applied: 1, state: "drift" },
						applyJob: {
							id: "job-1",
							desiredRevision: 2,
							baseRevision: 1,
							status: "failed",
							trigger: "mutation",
							createdAt: 1700000000,
						},
					},
					{ status: 201 },
				),
			),
		);
		renderNewClient();
		await user.type(await screen.findByLabelText(/^name$/i), "alice");
		await user.click(screen.getByRole("button", { name: /^next$/i }));
		await user.click(screen.getByRole("button", { name: /^next$/i }));
		await user.click(screen.getByRole("button", { name: /^review$/i }));
		await user.click(screen.getByRole("button", { name: /create client/i }));
		expect(await screen.findByText(/applying it failed/i)).toBeInTheDocument();
		expect(screen.getByText(/apply job job-1/i)).toBeInTheDocument();
		// No green "Client created" — the exact-match query proves the
		// success badge was not rendered.
		expect(screen.queryByText(/^client created$/i)).not.toBeInTheDocument();
	});

	it("keeps the green badge when create fully succeeds", async () => {
		const user = userEvent.setup();
		server.use(
			http.get("/api/inbounds", () => HttpResponse.json([])),
			http.get("/api/v1/clients", () =>
				HttpResponse.json({ items: [], total: 0, page: 1, pageSize: 25 }),
			),
			http.post("/api/v1/clients", () =>
				HttpResponse.json(
					{
						client: {
							id: "c1",
							name: "alice",
							enabled: true,
							version: 1,
							status: "active",
						},
						success: true,
						revision: { desired: 2, applied: 2, state: "applied" },
					},
					{ status: 201 },
				),
			),
		);
		renderNewClient();
		await user.type(await screen.findByLabelText(/^name$/i), "alice");
		await user.click(screen.getByRole("button", { name: /^next$/i }));
		await user.click(screen.getByRole("button", { name: /^next$/i }));
		await user.click(screen.getByRole("button", { name: /^review$/i }));
		await user.click(screen.getByRole("button", { name: /create client/i }));
		expect(await screen.findByText(/^client created$/i)).toBeInTheDocument();
		expect(screen.queryByText(/applying it failed/i)).not.toBeInTheDocument();
	});
});
